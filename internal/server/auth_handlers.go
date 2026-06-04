package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"friends-football/internal/database"
)

// Session secret key (in production, this should come from environment)
var sessionSecret = generateSessionSecret()

func generateSessionSecret() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// Fallback to a default (not recommended for production)
		return []byte("friends-football-secret-key-2024")
	}
	return key
}

// LoginRequest represents the login request body.
type LoginRequest struct {
	Phone string `json:"phone"`
}

// LoginResponse represents the login response.
type LoginResponse struct {
	PlayerID int    `json:"player_id"`
	Name     string `json:"name"`
	IsAdmin  bool   `json:"is_admin"`
}

// handleLogin authenticates a player by phone number.
//
//	POST /api/auth/login
//	Body: {"phone": "+972501234567"}
//	Response: {"player_id": 1, "name": "Omer"}
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Phone == "" {
		writeError(w, http.StatusBadRequest, "phone number is required")
		return
	}

	// Normalize phone number: trim whitespace and remove dashes
	phone := strings.ReplaceAll(strings.TrimSpace(req.Phone), "-", "")

	// Validate phone length (Israeli numbers: 10 digits like 0501234567)
	if len(phone) != 10 && len(phone) != 13 { // 10 for local, 13 for +972...
		writeError(w, http.StatusBadRequest, "phone number must be 10 digits (or 13 with country code)")
		return
	}

	// Look up player by phone
	player, err := getPlayerByPhone(phone)
	if err != nil {
		log.Printf("[Login] Failed login attempt - phone not found: %s", phone)
		writeError(w, http.StatusUnauthorized, "player not found")
		return
	}

	// Create session cookie
	sessionValue := createSessionToken(player.ID)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionValue,
		Path:     "/",
		MaxAge:   cookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	log.Printf("[Login] %s logged in", player.Name)

	writeJSON(w, http.StatusOK, LoginResponse{
		PlayerID: player.ID,
		Name:     player.Name,
		IsAdmin:  player.IsAdmin,
	})
}

// getPlayerByPhone retrieves a player from the database by phone number.
func getPlayerByPhone(phone string) (*database.Player, error) {
	var player database.Player
	query := fmt.Sprintf(`
		SELECT id, name, phone, nickname_aliases, base_skill_rating, base_fitness_rating, is_admin, tier
		FROM players WHERE phone = %s
	`, database.Placeholder(1))
	err := database.DB.QueryRow(query, phone).Scan(&player.ID, &player.Name, &player.Phone, &player.NicknameAliases,
		&player.BaseSkillRating, &player.BaseFitnessRating, &player.IsAdmin, &player.Tier)
	if err != nil {
		return nil, err
	}
	return &player, nil
}

// getPlayerName returns the player's name by ID, or "Unknown" if not found.
func getPlayerName(playerID int) string {
	var name string
	query := fmt.Sprintf(`SELECT name FROM players WHERE id = %s`, database.Placeholder(1))
	err := database.DB.QueryRow(query, playerID).Scan(&name)
	if err != nil {
		return "Unknown"
	}
	return name
}

// createSessionToken creates a signed session token containing the player ID.
func createSessionToken(playerID int) string {
	data := fmt.Sprintf("%d:%d", playerID, time.Now().Unix())
	signature := signData(data)
	return base64.URLEncoding.EncodeToString([]byte(data + "." + signature))
}

// signData creates an HMAC signature for the data.
func signData(data string) string {
	h := hmac.New(sha256.New, sessionSecret)
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// validateSessionToken validates and extracts the player ID from a session token.
func validateSessionToken(token string) (int, error) {
	decoded, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return 0, fmt.Errorf("invalid token encoding")
	}

	parts := strings.Split(string(decoded), ".")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid token format")
	}

	data, signature := parts[0], parts[1]

	// Verify signature
	expectedSig := signData(data)
	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		return 0, fmt.Errorf("invalid signature")
	}

	// Extract player ID
	dataParts := strings.Split(data, ":")
	if len(dataParts) != 2 {
		return 0, fmt.Errorf("invalid data format")
	}

	playerID, err := strconv.Atoi(dataParts[0])
	if err != nil {
		return 0, fmt.Errorf("invalid player ID")
	}

	return playerID, nil
}

// getSessionPlayerID extracts the player ID from the session cookie.
func getSessionPlayerID(r *http.Request) (int, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return 0, fmt.Errorf("no session cookie")
	}
	return validateSessionToken(cookie.Value)
}

// PlayerResponse represents a player in API responses.
type PlayerResponse struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Tier int    `json:"tier"` // 1=Core, 2=Regular, 3=Occasional, 4=Rare
	// Voter's existing ratings for this player (null if not rated)
	MySkillRating   *int    `json:"my_skill_rating,omitempty"`
	MyFitnessRating *string `json:"my_fitness_rating,omitempty"` // "Poor", "Normal", "Good"
}

// handleGetPlayers returns all players except the logged-in user, with voter's existing ratings.
// Results are sorted by tier ASC (Core first) then name ASC.
//
//	GET /api/players
//	Requires: session cookie
//	Response: [{"id": 1, "name": "Dan", "tier": 1, "my_skill_rating": 7, "my_fitness_rating": "Good"}, ...]
func (s *Server) handleGetPlayers(w http.ResponseWriter, r *http.Request) {
	voterID, err := getSessionPlayerID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	log.Printf("[Players] %s fetching player list", getPlayerName(voterID))

	// Get all players except the voter, sorted by tier ASC then name ASC
	query := fmt.Sprintf(`
		SELECT p.id, p.name, p.tier, ar.skill_rating, ar.fitness_rating
		FROM players p
		LEFT JOIN anonymous_ratings ar ON ar.target_id = p.id AND ar.voter_id = %s
		WHERE p.id != %s
		ORDER BY p.tier ASC, p.name ASC
	`, database.Placeholder(1), database.Placeholder(2))
	rows, err := database.DB.Query(query, voterID, voterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch players")
		return
	}
	defer rows.Close()

	var players []PlayerResponse
	for rows.Next() {
		var p PlayerResponse
		var skillRating, fitnessRating *int
		if err := rows.Scan(&p.ID, &p.Name, &p.Tier, &skillRating, &fitnessRating); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to scan player")
			return
		}
		if skillRating != nil {
			p.MySkillRating = skillRating
		}
		if fitnessRating != nil {
			category := mapFitnessToCategory(*fitnessRating)
			p.MyFitnessRating = &category
		}
		players = append(players, p)
	}

	if players == nil {
		players = []PlayerResponse{} // Return empty array instead of null
	}

	writeJSON(w, http.StatusOK, players)
}

// mapFitnessToCategory converts fitness integer to category string.
func mapFitnessToCategory(fitness int) string {
	switch fitness {
	case database.FitnessPoor:
		return "Poor"
	case database.FitnessNormal:
		return "Normal"
	case database.FitnessGood:
		return "Good"
	default:
		return "Normal"
	}
}

// RatingRequest represents a single rating in a batch submission.
type RatingRequest struct {
	TargetID        int    `json:"target_id"`
	SkillRating     int    `json:"skill_rating"`
	FitnessCategory string `json:"fitness_category"` // "Poor", "Normal", "Good"
}

// handleSubmitRatings saves or updates multiple anonymous peer ratings in a single transaction.
//
//	POST /api/ratings
//	Body: [{"target_id": 2, "skill_rating": 8, "fitness_category": "Good"}, ...]
//	Response: {"success": true, "saved_count": 5}
func (s *Server) handleSubmitRatings(w http.ResponseWriter, r *http.Request) {
	voterID, err := getSessionPlayerID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var ratings []RatingRequest
	if err := decodeJSON(r, &ratings); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: expected array of ratings")
		return
	}

	if len(ratings) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "saved_count": 0})
		return
	}

	// Validate all ratings before starting transaction
	for i, req := range ratings {
		if req.TargetID <= 0 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("rating %d: invalid target_id", i))
			return
		}
		if req.TargetID == voterID {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("rating %d: cannot rate yourself", i))
			return
		}
		if req.SkillRating < 1 || req.SkillRating > 10 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("rating %d: skill_rating must be between 1 and 10", i))
			return
		}
		if _, err := mapFitnessCategory(req.FitnessCategory); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("rating %d: %s", i, err.Error()))
			return
		}
	}

	// Begin transaction
	tx, err := database.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()

	// Prepare statement for upserts
	var upsertQuery string
	if database.ActiveDriver == database.DriverPostgres {
		upsertQuery = `
			INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT(voter_id, target_id) DO UPDATE SET
				skill_rating = EXCLUDED.skill_rating,
				fitness_rating = EXCLUDED.fitness_rating
		`
	} else {
		upsertQuery = `
			INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(voter_id, target_id) DO UPDATE SET
				skill_rating = excluded.skill_rating,
				fitness_rating = excluded.fitness_rating
		`
	}
	stmt, err := tx.Prepare(upsertQuery)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare statement")
		return
	}
	defer stmt.Close()

	// Execute all upserts
	for _, req := range ratings {
		fitnessRating, _ := mapFitnessCategory(req.FitnessCategory) // Already validated
		if _, err := stmt.Exec(voterID, req.TargetID, req.SkillRating, fitnessRating); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save rating")
			return
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit ratings")
		return
	}

	log.Printf("[Ratings] %s submitted %d ratings", getPlayerName(voterID), len(ratings))

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "saved_count": len(ratings)})
}

// mapFitnessCategory converts a fitness category string to its integer value.
func mapFitnessCategory(category string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "poor":
		return database.FitnessPoor, nil
	case "normal":
		return database.FitnessNormal, nil
	case "good":
		return database.FitnessGood, nil
	default:
		return 0, fmt.Errorf("fitness_category must be 'Poor', 'Normal', or 'Good'")
	}
}
