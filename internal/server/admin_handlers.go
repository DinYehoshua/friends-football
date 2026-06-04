package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"friends-football/internal/balancer"
	"friends-football/internal/database"
	"friends-football/internal/parser"
)

// UploadPlayerResponse represents a player in the upload response (ID + name only).
type UploadPlayerResponse struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// UploadResponse represents the response from chat upload.
type UploadResponse struct {
	Players           []UploadPlayerResponse `json:"players"`
	Unresolved        []UnresolvedResponse   `json:"unresolved,omitempty"`
	RegisteredPlayers []RegisteredPlayer     `json:"registered_players,omitempty"` // Only included when unresolved is not empty
}

// UnresolvedResponse represents an unresolved player name.
type UnresolvedResponse struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
}

// RegisteredPlayer represents a player available for alias mapping.
type RegisteredPlayer struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// handleUploadChat processes an uploaded WhatsApp chat zip file.
//
//	POST /api/admin/upload
//	Content-Type: multipart/form-data
//	Form field: "chat_file" (the .zip file)
//	Response: {"players": [...], "unresolved": [...]}
func (s *Server) handleUploadChat(w http.ResponseWriter, r *http.Request) {
	adminID, _ := getSessionPlayerID(r)
	adminName := getPlayerName(adminID)
	log.Printf("[Upload] %s starting chat upload", adminName)
	start := time.Now()

	// Parse multipart form (max 10MB)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse form data")
		return
	}

	// Get uploaded file
	file, _, err := r.FormFile("chat_file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "chat_file is required")
		return
	}
	defer file.Close()

	// Read file content
	zipData, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	log.Printf("[Upload] Read %d bytes from zip file", len(zipData))

	// Extract chat content from zip
	chatContent, err := parser.ExtractChatFromZipBytes(zipData)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid zip file: "+err.Error())
		return
	}
	log.Printf("[Upload] Extracted chat content (%d chars)", len(chatContent))

	// Create parser and parse chat
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	p, err := parser.New(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to initialize parser: "+err.Error())
		return
	}
	defer p.Close()

	// Parse and resolve players
	log.Println("[Upload] Calling Gemini API to parse chat...")
	result, err := p.ParseAndResolve(ctx, chatContent)
	if err != nil {
		log.Printf("[Upload] Gemini parsing failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to parse chat: "+err.Error())
		return
	}
	log.Printf("[Upload] Resolved %d players, %d unresolved (took %v)", len(result.Players), len(result.UnresolvedPlayers), time.Since(start))

	// Build response
	response := UploadResponse{
		Players: make([]UploadPlayerResponse, 0, len(result.Players)),
	}

	for _, player := range result.Players {
		response.Players = append(response.Players, UploadPlayerResponse{
			ID:   player.ID,
			Name: player.Name,
		})
	}

	for _, unresolved := range result.UnresolvedPlayers {
		response.Unresolved = append(response.Unresolved, UnresolvedResponse{
			Name:  unresolved.ExtractedName,
			Index: unresolved.Index,
		})
	}

	// If there are unresolved players, include all registered players for alias mapping
	if len(response.Unresolved) > 0 {
		registeredPlayers, err := getAllRegisteredPlayers()
		if err != nil {
			log.Printf("[Upload] Warning: failed to fetch registered players: %v", err)
		} else {
			response.RegisteredPlayers = registeredPlayers
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// PlayerInput represents a player in the generate teams request.
// Either ID or Name must be provided. If ID > 0, lookup by ID. Otherwise, resolve by name.
// For unrecognized players, Skill and Fitness can be provided to override defaults.
type PlayerInput struct {
	ID      int    `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Skill   *int   `json:"skill,omitempty"`   // Temporary skill (1-10) for unrecognized players
	Fitness *int   `json:"fitness,omitempty"` // Temporary fitness (1-3) for unrecognized players
}

// GenerateTeamsRequest represents the request body for team generation.
type GenerateTeamsRequest struct {
	Players         []PlayerInput `json:"players"`
	ConsiderFitness bool          `json:"consider_fitness"`
}

// TeamPlayerResponse represents a player in a generated team response.
type TeamPlayerResponse struct {
	ID                int     `json:"id"`
	Name              string  `json:"name"`
	BaseSkillRating   float64 `json:"base_skill_rating"`
	BaseFitnessRating float64 `json:"base_fitness_rating"`
}

// TeamResponse represents a team in the API response.
type TeamResponse struct {
	Players      []TeamPlayerResponse `json:"players"`
	TotalSkill   float64              `json:"total_skill"`
	TotalFitness float64              `json:"total_fitness"`
}

// GenerateTeamsResponse represents the response from team generation.
type GenerateTeamsResponse struct {
	Home       TeamResponse `json:"home"`
	Away       TeamResponse `json:"away"`
	SkillDelta float64      `json:"skill_delta"`
	CostDelta  float64      `json:"fitness_delta"`
	TotalCost  float64      `json:"total_cost"`
}

// handleGenerateTeams generates balanced teams from 12 players.
// Players can be specified by ID (from upload response) or by name (if admin edited manually).
//
//	POST /api/admin/generate-teams
//	Body: {"players": [{"id": 1}, {"id": 2}, {"name": "Guest Player"}, ...], "consider_fitness": true}
//	Response: {"home": {...}, "away": {...}, "skill_delta": 0.5, "total_cost": 1.5}
func (s *Server) handleGenerateTeams(w http.ResponseWriter, r *http.Request) {
	adminID, _ := getSessionPlayerID(r)
	adminName := getPlayerName(adminID)
	log.Printf("[Generate] %s starting team generation", adminName)
	start := time.Now()

	var req GenerateTeamsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate player count
	if len(req.Players) != 12 {
		writeError(w, http.StatusBadRequest, "exactly 12 players are required")
		return
	}

	// Step 1: Resolve all players from DB (by ID or name)
	dbPlayers, err := resolvePlayers(req.Players)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Collect IDs for rating lookup (only real players, not guests)
	playerIDs := make([]int, 0, len(dbPlayers))
	guestCount := 0
	for _, p := range dbPlayers {
		if p.ID > 0 {
			playerIDs = append(playerIDs, p.ID)
		} else {
			guestCount++
		}
	}
	log.Printf("[Generate] Resolved %d DB players, %d guests", len(playerIDs), guestCount)

	// Step 2: Get average ratings from anonymous_ratings (read-only)
	var avgRatings map[int]database.PlayerRatingAvg
	if len(playerIDs) > 0 {
		avgRatings, err = database.ComputeAverageRatings(playerIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to compute ratings: "+err.Error())
			return
		}
	}

	// Step 3: Build balancer players, using peer ratings if available, else base ratings
	players := make([]balancer.Player, len(dbPlayers))
	for i, p := range dbPlayers {
		skillRating := p.BaseSkillRating
		fitnessRating := p.BaseFitnessRating

		if avg, ok := avgRatings[p.ID]; ok {
			skillRating = avg.AvgSkillRating
			fitnessRating = avg.AvgFitnessRating
		}

		players[i] = balancer.Player{
			ID:            p.ID,
			Name:          p.Name,
			SkillRating:   skillRating,
			FitnessRating: fitnessRating,
		}
	}

	// Step 4: Generate balanced teams
	result, err := balancer.GenerateTeams(players, req.ConsiderFitness)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate teams: "+err.Error())
		return
	}
	log.Printf("[Generate] Teams balanced: skill_delta=%.2f, cost=%.2f (took %v)", result.SkillDelta, result.TotalCost, time.Since(start))

	// Build response
	response := GenerateTeamsResponse{
		Home:       buildTeamResponse(result.Home),
		Away:       buildTeamResponse(result.Away),
		SkillDelta: result.SkillDelta,
		CostDelta:  result.CostDelta,
		TotalCost:  result.TotalCost,
	}

	writeJSON(w, http.StatusOK, response)
}

// resolvePlayers converts PlayerInput slice to database.Player slice.
// Players with ID > 0 are fetched by ID; others are resolved by name/nickname.
func resolvePlayers(inputs []PlayerInput) ([]database.Player, error) {
	var result []database.Player
	var idsToFetch []int
	var namesToResolve []struct {
		index int
		name  string
	}

	// Categorize inputs
	for i, input := range inputs {
		if input.ID > 0 {
			idsToFetch = append(idsToFetch, input.ID)
		} else if input.Name != "" {
			namesToResolve = append(namesToResolve, struct {
				index int
				name  string
			}{i, input.Name})
		} else {
			return nil, fmt.Errorf("player at index %d has neither id nor name", i)
		}
	}

	// Fetch players by ID
	var playersByID map[int]database.Player
	if len(idsToFetch) > 0 {
		players, err := database.GetPlayersByIDs(idsToFetch)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch players by ID: %w", err)
		}
		playersByID = make(map[int]database.Player, len(players))
		for _, p := range players {
			playersByID[p.ID] = p
		}
	}

	// Resolve players by name using parser's matching logic
	var resolvedByName map[string]database.Player
	if len(namesToResolve) > 0 {
		names := make([]string, len(namesToResolve))
		for i, n := range namesToResolve {
			names[i] = n.name
		}
		parseResult, err := parser.ResolvePlayersFromDB(names)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve players by name: %w", err)
		}
		resolvedByName = make(map[string]database.Player, len(names))
		for i, p := range parseResult.Players {
			resolvedByName[names[i]] = p
		}
	}

	// Build result in original order
	result = make([]database.Player, len(inputs))
	for i, input := range inputs {
		if input.ID > 0 {
			p, ok := playersByID[input.ID]
			if !ok {
				return nil, fmt.Errorf("player with ID %d not found", input.ID)
			}
			result[i] = p
		} else {
			p := resolvedByName[input.Name]
			if p.ID == -1 {
				// Unresolved - create guest player with provided or default ratings
				skillRating := 5.0
				fitnessRating := 2.0
				if input.Skill != nil {
					skillRating = float64(*input.Skill)
				}
				if input.Fitness != nil {
					fitnessRating = float64(*input.Fitness)
				}
				result[i] = database.Player{
					ID:                -1,
					Name:              input.Name,
					BaseSkillRating:   skillRating,
					BaseFitnessRating: fitnessRating,
				}
			} else {
				result[i] = p
			}
		}
	}

	return result, nil
}

// buildTeamResponse converts balancer.Team to TeamResponse.
func buildTeamResponse(team balancer.Team) TeamResponse {
	players := make([]TeamPlayerResponse, len(team.Players))
	for i, p := range team.Players {
		players[i] = TeamPlayerResponse{
			ID:                p.ID,
			Name:              p.Name,
			BaseSkillRating:   p.SkillRating,
			BaseFitnessRating: p.FitnessRating,
		}
	}
	return TeamResponse{
		Players:      players,
		TotalSkill:   team.TotalSkill,
		TotalFitness: team.TotalFitness,
	}
}

// RecalculateBaseRatingsResponse represents the response from base ratings recalculation.
type RecalculateBaseRatingsResponse struct {
	Success      bool `json:"success"`
	PlayersCount int  `json:"players_updated"`
}

// handleRecalculateBaseRatings triggers a full recalculation of base_skill_rating and
// base_fitness_rating for all players based on their anonymous peer ratings.
//
//	POST /api/admin/recalculate-base-ratings
//	Requires: session cookie (admin only)
//	Response: {"success": true, "players_updated": 15}
func (s *Server) handleRecalculateBaseRatings(w http.ResponseWriter, r *http.Request) {
	adminID, _ := getSessionPlayerID(r)
	adminName := getPlayerName(adminID)
	log.Printf("[Recalculate] %s starting base ratings recalculation", adminName)
	count, err := database.RecalculateAllBaseRatings()
	if err != nil {
		log.Printf("[Recalculate] Failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to recalculate ratings: "+err.Error())
		return
	}
	log.Printf("[Recalculate] Updated %d players", count)

	writeJSON(w, http.StatusOK, RecalculateBaseRatingsResponse{
		Success:      true,
		PlayersCount: count,
	})
}

// getAllRegisteredPlayers fetches all players from the database for alias mapping dropdown.
func getAllRegisteredPlayers() ([]RegisteredPlayer, error) {
	query := `SELECT id, name FROM players ORDER BY name ASC`
	rows, err := database.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var players []RegisteredPlayer
	for rows.Next() {
		var p RegisteredPlayer
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		players = append(players, p)
	}
	return players, rows.Err()
}

// ResolveAliasRequest represents a single alias resolution in the batch.
type ResolveAliasRequest struct {
	UnresolvedName string `json:"unresolved_name"`
	PlayerID       int    `json:"player_id"` // 0 or -1 means "guest"
	Index          int    `json:"index"`     // Original index in the players list
}

// ResolveAliasesRequest represents the request body for alias resolution.
type ResolveAliasesRequest struct {
	Mappings []ResolveAliasRequest `json:"mappings"`
}

// ResolveAliasesResponse represents the response from alias resolution.
type ResolveAliasesResponse struct {
	Success      bool                   `json:"success"`
	AliasesSaved int                    `json:"aliases_saved"`
	Players      []UploadPlayerResponse `json:"players"` // Updated player list with resolved IDs
}

// handleResolveAliases processes alias mappings from the "Who Are They?" screen.
// For each mapping:
// - If PlayerID > 0: append the unresolved name to that player's nickname_aliases
// - If PlayerID <= 0: treat as guest (no DB change)
//
//	POST /api/admin/resolve-aliases
//	Body: {"mappings": [{"unresolved_name": "יוסי", "player_id": 5, "index": 2}, ...]}
//	Response: {"success": true, "aliases_saved": 2, "players": [...]}
func (s *Server) handleResolveAliases(w http.ResponseWriter, r *http.Request) {
	adminID, _ := getSessionPlayerID(r)
	adminName := getPlayerName(adminID)
	log.Printf("[ResolveAliases] %s starting alias resolution", adminName)

	var req ResolveAliasesRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Mappings) == 0 {
		writeJSON(w, http.StatusOK, ResolveAliasesResponse{
			Success:      true,
			AliasesSaved: 0,
			Players:      []UploadPlayerResponse{},
		})
		return
	}

	// Separate mappings into those that need DB update vs guests
	aliasUpdates := make(map[int][]string) // playerID -> list of new aliases
	for _, m := range req.Mappings {
		if m.PlayerID > 0 && m.UnresolvedName != "" {
			aliasUpdates[m.PlayerID] = append(aliasUpdates[m.PlayerID], m.UnresolvedName)
		}
	}

	// Update aliases in database
	aliasesSaved := 0
	if len(aliasUpdates) > 0 {
		count, err := appendPlayerAliases(aliasUpdates)
		if err != nil {
			log.Printf("[ResolveAliases] Failed to save aliases: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to save aliases: "+err.Error())
			return
		}
		aliasesSaved = count
	}

	// Build response with updated player info
	// For mappings with PlayerID > 0, fetch the player's real name
	// For guests (PlayerID <= 0), keep the unresolved name with ID = -1
	playerIDs := make([]int, 0)
	for _, m := range req.Mappings {
		if m.PlayerID > 0 {
			playerIDs = append(playerIDs, m.PlayerID)
		}
	}

	playerMap := make(map[int]string) // ID -> Name
	if len(playerIDs) > 0 {
		players, err := database.GetPlayersByIDs(playerIDs)
		if err != nil {
			log.Printf("[ResolveAliases] Warning: failed to fetch player names: %v", err)
		} else {
			for _, p := range players {
				playerMap[p.ID] = p.Name
			}
		}
	}

	// Build result players list
	resultPlayers := make([]UploadPlayerResponse, len(req.Mappings))
	for i, m := range req.Mappings {
		if m.PlayerID > 0 {
			name := playerMap[m.PlayerID]
			if name == "" {
				name = m.UnresolvedName // Fallback
			}
			resultPlayers[i] = UploadPlayerResponse{
				ID:   m.PlayerID,
				Name: name,
			}
		} else {
			// Guest player
			resultPlayers[i] = UploadPlayerResponse{
				ID:   -1,
				Name: m.UnresolvedName,
			}
		}
	}

	log.Printf("[ResolveAliases] %s saved %d aliases", adminName, aliasesSaved)

	writeJSON(w, http.StatusOK, ResolveAliasesResponse{
		Success:      true,
		AliasesSaved: aliasesSaved,
		Players:      resultPlayers,
	})
}

// appendPlayerAliases appends new aliases to existing players' nickname_aliases JSON arrays.
// Returns the count of players updated.
func appendPlayerAliases(updates map[int][]string) (int, error) {
	tx, err := database.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	count := 0
	for playerID, newAliases := range updates {
		// Get current aliases
		var currentAliasesJSON sql.NullString
		query := fmt.Sprintf(`SELECT nickname_aliases FROM players WHERE id = %s`, database.Placeholder(1))
		err := tx.QueryRow(query, playerID).Scan(&currentAliasesJSON)
		if err != nil {
			return 0, fmt.Errorf("failed to fetch player %d: %w", playerID, err)
		}

		// Parse existing aliases
		var aliases []string
		if currentAliasesJSON.Valid && currentAliasesJSON.String != "" {
			if err := json.Unmarshal([]byte(currentAliasesJSON.String), &aliases); err != nil {
				aliases = []string{} // Start fresh if parse fails
			}
		}

		// Append new aliases (avoid duplicates)
		existingSet := make(map[string]bool)
		for _, a := range aliases {
			existingSet[strings.ToLower(a)] = true
		}
		for _, newAlias := range newAliases {
			if !existingSet[strings.ToLower(newAlias)] {
				aliases = append(aliases, newAlias)
			}
		}

		// Serialize and update
		aliasesJSON, err := json.Marshal(aliases)
		if err != nil {
			return 0, fmt.Errorf("failed to marshal aliases: %w", err)
		}

		updateQuery := fmt.Sprintf(`UPDATE players SET nickname_aliases = %s WHERE id = %s`,
			database.Placeholder(1), database.Placeholder(2))
		if _, err := tx.Exec(updateQuery, string(aliasesJSON), playerID); err != nil {
			return 0, fmt.Errorf("failed to update player %d: %w", playerID, err)
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return count, nil
}
