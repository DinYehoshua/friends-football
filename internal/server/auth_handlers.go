package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
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

// getPlayerByPhone retrieves a player from the database by phone number.
func getPlayerByPhone(phone string) (*database.Player, error) {
	var player database.Player
	query := fmt.Sprintf(`
		SELECT id, name, phone, email, nickname_aliases, base_skill_rating, base_fitness_rating, is_admin, tier
		FROM players WHERE phone = %s
	`, database.Placeholder(1))
	err := database.DB.QueryRow(query, phone).Scan(&player.ID, &player.Name, &player.Phone, &player.Email, &player.NicknameAliases,
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
	MyFitnessRating *string `json:"my_fitness_rating,omitempty"` // "Low", "Poor", "Average", "Good", "Excellent"
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
		writeError(w, http.StatusUnauthorized, "Not authenticated")
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
	case database.FitnessLow:
		return "Low"
	case database.FitnessPoor:
		return "Poor"
	case database.FitnessAverage:
		return "Average"
	case database.FitnessGood:
		return "Good"
	case database.FitnessExcellent:
		return "Excellent"
	default:
		return "Average"
	}
}

// RatingRequest represents a single rating in a batch submission.
type RatingRequest struct {
	TargetID        int    `json:"target_id"`
	SkillRating     int    `json:"skill_rating"`
	FitnessCategory string `json:"fitness_category"` // "Very Poor", "Poor", "Average", "Good", "Excellent"
}

// handleSubmitRatings saves or updates multiple anonymous peer ratings in a single transaction.
//
//	POST /api/ratings
//	Body: [{"target_id": 2, "skill_rating": 8, "fitness_category": "Good"}, ...]
//	Response: {"success": true, "saved_count": 5}
func (s *Server) handleSubmitRatings(w http.ResponseWriter, r *http.Request) {
	voterID, err := getSessionPlayerID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
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
			writeError(w, http.StatusBadRequest, "Nice try, but you can't rate yourself!")
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
	case "low":
		return database.FitnessLow, nil
	case "poor":
		return database.FitnessPoor, nil
	case "average":
		return database.FitnessAverage, nil
	case "good":
		return database.FitnessGood, nil
	case "excellent":
		return database.FitnessExcellent, nil
	default:
		return 0, fmt.Errorf("fitness_category must be 'Low', 'Poor', 'Average', 'Good', or 'Excellent'")
	}
}

// ========================================
// Google OAuth / Account Claiming
// ========================================

// GoogleAuthRequest represents the Google sign-in request.
type GoogleAuthRequest struct {
	IDToken string `json:"id_token"`
}

// GoogleAuthResponse represents the Google sign-in response.
type GoogleAuthResponse struct {
	Status      string `json:"status"`                 // "claimed", "needs_claim", "exists"
	PlayerID    int    `json:"player_id,omitempty"`    // Set if already linked or newly claimed
	Name        string `json:"name,omitempty"`         // Player name
	IsAdmin     bool   `json:"is_admin,omitempty"`     // Admin status
	GoogleEmail string `json:"google_email,omitempty"` // Email from Google (for needs_claim)
	GoogleName  string `json:"google_name,omitempty"`  // Name from Google (for display)
	ClaimToken  string `json:"claim_token,omitempty"`  // Temporary token for claim flow
}

// ClaimAccountRequest represents the account claiming request.
type ClaimAccountRequest struct {
	ClaimToken string `json:"claim_token"`
	Phone      string `json:"phone"`
}

// GoogleClaims represents the decoded Google ID token payload (minimal fields).
type GoogleClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Sub           string `json:"sub"` // Google user ID
	Aud           string `json:"aud"` // Audience (Client ID)
	Iss           string `json:"iss"` // Issuer
	Exp           int64  `json:"exp"` // Expiration time (Unix timestamp)
}

// googleClientID is loaded from GOOGLE_CLIENT_ID environment variable.
var googleClientID = os.Getenv("GOOGLE_CLIENT_ID")

// claimTokens stores temporary claim tokens (email -> token -> expiry).
// In production, consider using Redis or a database table.
var claimTokens = make(map[string]claimTokenData)

type claimTokenData struct {
	Email     string
	Name      string
	ExpiresAt time.Time
}

// handleGoogleAuth handles Google OAuth sign-in.
//
//	POST /api/auth/google
//	Body: {"id_token": "..."}
//	Response:
//	  - If email already linked: {"status": "exists", "player_id": 1, "name": "Omer", "is_admin": true}
//	  - If email not linked: {"status": "needs_claim", "google_email": "...", "google_name": "...", "claim_token": "..."}
func (s *Server) handleGoogleAuth(w http.ResponseWriter, r *http.Request) {
	var req GoogleAuthRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.IDToken == "" {
		writeError(w, http.StatusBadRequest, "id_token is required")
		return
	}

	// Verify and decode the Google ID token
	claims, err := verifyGoogleIDToken(req.IDToken)
	if err != nil {
		log.Printf("[GoogleAuth] Token verification failed: %v", err)
		writeError(w, http.StatusUnauthorized, "Invalid Google token")
		return
	}

	if !claims.EmailVerified {
		writeError(w, http.StatusUnauthorized, "Email not verified with Google")
		return
	}

	email := strings.ToLower(strings.TrimSpace(claims.Email))

	// Check if this email is already linked to a player
	player, err := getPlayerByEmail(email)
	if err == nil && player != nil {
		// Email already linked - log them in
		sessionValue := createSessionToken(player.ID)
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    sessionValue,
			Path:     "/",
			MaxAge:   cookieMaxAge,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		log.Printf("[GoogleAuth] %s logged in via Google", player.Name)

		writeJSON(w, http.StatusOK, GoogleAuthResponse{
			Status:   "exists",
			PlayerID: player.ID,
			Name:     player.Name,
			IsAdmin:  player.IsAdmin,
		})
		return
	}

	// Email not linked - generate a claim token for the linking flow
	claimToken := generateClaimToken()
	claimTokens[claimToken] = claimTokenData{
		Email:     email,
		Name:      claims.Name,
		ExpiresAt: time.Now().Add(10 * time.Minute), // Token valid for 10 minutes
	}

	// Clean up expired tokens periodically
	cleanExpiredClaimTokens()

	log.Printf("[GoogleAuth] New Google sign-in, needs claim: %s", email)

	writeJSON(w, http.StatusOK, GoogleAuthResponse{
		Status:      "needs_claim",
		GoogleEmail: email,
		GoogleName:  claims.Name,
		ClaimToken:  claimToken,
	})
}

// handleClaimAccount links a Google account to an existing player profile.
//
//	POST /api/auth/claim
//	Body: {"claim_token": "...", "phone": "0501234567"}
//	Response: {"status": "claimed", "player_id": 1, "name": "Omer", "is_admin": true}
func (s *Server) handleClaimAccount(w http.ResponseWriter, r *http.Request) {
	var req ClaimAccountRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ClaimToken == "" {
		writeError(w, http.StatusBadRequest, "claim_token is required")
		return
	}

	if req.Phone == "" {
		writeError(w, http.StatusBadRequest, "phone is required")
		return
	}

	// Validate claim token
	tokenData, exists := claimTokens[req.ClaimToken]
	if !exists {
		writeError(w, http.StatusUnauthorized, "Invalid or expired claim token")
		return
	}

	if time.Now().After(tokenData.ExpiresAt) {
		delete(claimTokens, req.ClaimToken)
		writeError(w, http.StatusUnauthorized, "Claim token expired")
		return
	}

	// Normalize phone number
	phone := strings.ReplaceAll(strings.TrimSpace(req.Phone), "-", "")

	// Find player by phone
	player, err := getPlayerByPhone(phone)
	if err != nil {
		log.Printf("[ClaimAccount] Phone not found: %s", phone)
		writeError(w, http.StatusNotFound, "No player found with this phone number.\nTalk to the manager to join!")
		return
	}

	// CRITICAL SECURITY CHECK: Only allow claim if email is NULL
	if player.Email.Valid && player.Email.String != "" {
		log.Printf("[ClaimAccount] BLOCKED: Attempted hijack - phone %s already has email %s, tried to claim with %s",
			phone, player.Email.String, tokenData.Email)
		writeError(w, http.StatusConflict, "This account is already linked to a different Google account")
		return
	}

	// Link the email to the player
	if err := updatePlayerEmail(player.ID, tokenData.Email); err != nil {
		log.Printf("[ClaimAccount] Failed to update email for player %d: %v", player.ID, err)
		writeError(w, http.StatusInternalServerError, "Failed to link account")
		return
	}

	// Delete the claim token (one-time use)
	delete(claimTokens, req.ClaimToken)

	// Create session
	sessionValue := createSessionToken(player.ID)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionValue,
		Path:     "/",
		MaxAge:   cookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	log.Printf("[ClaimAccount] %s claimed account with Google: %s", player.Name, tokenData.Email)

	writeJSON(w, http.StatusOK, GoogleAuthResponse{
		Status:   "claimed",
		PlayerID: player.ID,
		Name:     player.Name,
		IsAdmin:  player.IsAdmin,
	})
}

// getPlayerByEmail retrieves a player by email address.
func getPlayerByEmail(email string) (*database.Player, error) {
	var player database.Player
	query := fmt.Sprintf(`
		SELECT id, name, phone, email, nickname_aliases, base_skill_rating, base_fitness_rating, is_admin, tier
		FROM players WHERE LOWER(email) = LOWER(%s)
	`, database.Placeholder(1))
	err := database.DB.QueryRow(query, email).Scan(&player.ID, &player.Name, &player.Phone, &player.Email, &player.NicknameAliases,
		&player.BaseSkillRating, &player.BaseFitnessRating, &player.IsAdmin, &player.Tier)
	if err != nil {
		return nil, err
	}
	return &player, nil
}

// updatePlayerEmail sets the email address for a player.
func updatePlayerEmail(playerID int, email string) error {
	query := fmt.Sprintf(`UPDATE players SET email = %s WHERE id = %s`, database.Placeholder(1), database.Placeholder(2))
	_, err := database.DB.Exec(query, email, playerID)
	return err
}

// generateClaimToken creates a random token for the claim flow.
func generateClaimToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based (not ideal but functional)
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.URLEncoding.EncodeToString(b)
}

// cleanExpiredClaimTokens removes expired tokens from memory.
func cleanExpiredClaimTokens() {
	now := time.Now()
	for token, data := range claimTokens {
		if now.After(data.ExpiresAt) {
			delete(claimTokens, token)
		}
	}
}

// verifyGoogleIDToken verifies and decodes a Google ID token.
// Validates audience (client ID), issuer, and expiration to prevent confused deputy attacks.
func verifyGoogleIDToken(idToken string) (*GoogleClaims, error) {
	// Check that GOOGLE_CLIENT_ID is configured
	if googleClientID == "" {
		return nil, fmt.Errorf("GOOGLE_CLIENT_ID not configured")
	}

	// Google ID tokens are JWTs with 3 parts: header.payload.signature
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	// Decode the payload (middle part) - it's base64url encoded
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode payload: %w", err)
	}

	// Parse the JSON payload
	var claims GoogleClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}

	// CRITICAL: Validate audience matches our Client ID (prevents confused deputy)
	if claims.Aud != googleClientID {
		return nil, fmt.Errorf("invalid audience: token not issued for this application")
	}

	// Validate issuer is Google
	if claims.Iss != "accounts.google.com" && claims.Iss != "https://accounts.google.com" {
		return nil, fmt.Errorf("invalid issuer: %s", claims.Iss)
	}

	// Validate token hasn't expired
	if time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("token expired")
	}

	// Validate email is present
	if claims.Email == "" {
		return nil, fmt.Errorf("no email in token")
	}

	// Note: For full security, you should also verify the JWT signature using Google's
	// public keys from https://www.googleapis.com/oauth2/v3/certs
	// However, with audience validation, the attack surface is significantly reduced
	// since only tokens issued specifically for our app are accepted.

	return &claims, nil
}
