package server

import (
	"log"
	"net/http"

	"friends-football/internal/database"
)

// DraftResponse represents the current draft state.
type DraftResponse struct {
	MatchDate  string   `json:"match_date"`
	PlayerIDs  []int    `json:"player_ids"`
	GuestNames []string `json:"guest_names"`
	BlueTeam   []string `json:"blue_team,omitempty"`
	WhiteTeam  []string `json:"white_team,omitempty"`
}

// SaveDraftRequest represents the request to save a draft.
type SaveDraftRequest struct {
	PlayerIDs  []int    `json:"player_ids"`
	GuestNames []string `json:"guest_names"`
}

// SaveTeamsRequest represents the request to save teams (names only).
type SaveTeamsRequest struct {
	BlueTeam  []string `json:"blue_team"`
	WhiteTeam []string `json:"white_team"`
}

// handleGetCurrentDraft returns the draft for the upcoming Saturday.
//
//	GET /api/draft/current
//	Requires: session cookie
//	Response: {"match_date": "2026-06-27", "player_ids": [1,2,3], "guest_names": ["Guest 1"]}
func (s *Server) handleGetCurrentDraft(w http.ResponseWriter, r *http.Request) {
	playerID, err := getSessionPlayerID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	draft, err := database.GetCurrentDraft()
	if err != nil {
		log.Printf("[Draft] Error fetching draft: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to fetch draft")
		return
	}

	// Return empty state if no draft exists
	if draft == nil {
		log.Printf("[Draft] %s fetched empty draft for %s", getPlayerName(playerID), database.GetUpcomingSaturday())
		writeJSON(w, http.StatusOK, DraftResponse{
			MatchDate:  database.GetUpcomingSaturday(),
			PlayerIDs:  []int{},
			GuestNames: []string{},
		})
		return
	}

	log.Printf("[Draft] %s fetched draft for %s: %d players, %d guests, teams saved: %v",
		getPlayerName(playerID), draft.MatchDate, len(draft.PlayerIDs), len(draft.GuestNames), len(draft.BlueTeam) > 0)

	writeJSON(w, http.StatusOK, DraftResponse{
		MatchDate:  draft.MatchDate,
		PlayerIDs:  draft.PlayerIDs,
		GuestNames: draft.GuestNames,
		BlueTeam:   draft.BlueTeam,
		WhiteTeam:  draft.WhiteTeam,
	})
}

// handleSaveCurrentDraft saves the draft for the upcoming Saturday.
//
//	POST /api/draft/current
//	Requires: session cookie
//	Body: {"player_ids": [1,2,3], "guest_names": ["Guest 1"]}
//	Response: {"success": true, "match_date": "2026-06-27"}
func (s *Server) handleSaveCurrentDraft(w http.ResponseWriter, r *http.Request) {
	playerID, err := getSessionPlayerID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var req SaveDraftRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Ensure slices are not nil
	if req.PlayerIDs == nil {
		req.PlayerIDs = []int{}
	}
	if req.GuestNames == nil {
		req.GuestNames = []string{}
	}

	err = database.SaveCurrentDraft(req.PlayerIDs, req.GuestNames)
	if err != nil {
		log.Printf("[Draft] Error saving draft: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to save draft")
		return
	}

	matchDate := database.GetUpcomingSaturday()
	log.Printf("[Draft] %s saved draft for %s: %d players, %d guests",
		getPlayerName(playerID), matchDate, len(req.PlayerIDs), len(req.GuestNames))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"match_date": matchDate,
	})
}

// handleSaveTeams saves the final team assignments for the upcoming Saturday.
//
//	POST /api/draft/teams
//	Requires: session cookie (admin only)
//	Body: {"blue_team": ["Player1", "Player2"], "white_team": ["Player3", "Player4"]}
//	Response: {"success": true, "match_date": "2026-06-27"}
func (s *Server) handleSaveTeams(w http.ResponseWriter, r *http.Request) {
	playerID, err := getSessionPlayerID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var req SaveTeamsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	err = database.SaveTeams(req.BlueTeam, req.WhiteTeam)
	if err != nil {
		log.Printf("[Draft] Error saving teams: %v", err)
		writeError(w, http.StatusInternalServerError, "Failed to save teams")
		return
	}

	matchDate := database.GetUpcomingSaturday()
	log.Printf("[Draft] %s saved teams for %s: %d blue, %d white",
		getPlayerName(playerID), matchDate, len(req.BlueTeam), len(req.WhiteTeam))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"match_date": matchDate,
	})
}
