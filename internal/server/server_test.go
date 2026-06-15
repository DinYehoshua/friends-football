package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"friends-football/internal/database"
)

// Shared test server instance
var testServer *Server

var _ = BeforeEach(func() {
	err := database.Init(":memory:")
	Expect(err).NotTo(HaveOccurred())
	seedTestPlayers()
	testServer = New(Config{Port: ":0"}) // StaticFiles left empty for tests
})

var _ = AfterEach(func() {
	database.Close()
})

// seedTestPlayers populates the database with test players
func seedTestPlayers() {
	players := []struct {
		name    string
		phone   string
		aliases string
		isAdmin int
		tier    int
	}{
		{"Omer", "+972501111111", `["Omeri"]`, 0, 1}, // Core
		{"Dan", "+972502222222", `["Danny"]`, 0, 1},  // Core
		{"Niv", "+972503333333", `["Nivi"]`, 0, 2},   // Regular
		{"Yossi", "+972504444444", `[]`, 0, 2},       // Regular
		{"Amit", "+972505555555", `[]`, 0, 2},        // Regular
		{"Roi", "+972506666666", `[]`, 0, 3},         // Occasional
		{"Gal", "+972507777777", `[]`, 0, 3},         // Occasional
		{"Tomer", "+972508888888", `[]`, 0, 3},       // Occasional
		{"Oren", "+972509999999", `[]`, 0, 3},        // Occasional
		{"Ben", "+972500000000", `[]`, 0, 4},         // Rare
		{"Lior", "+972501010101", `[]`, 0, 4},        // Rare
		{"Shai", "+972502020202", `[]`, 0, 4},        // Rare
		{"Admin", "+972509090909", `[]`, 1, 1},       // Admin (Core)
	}

	for _, p := range players {
		database.DB.Exec(`
			INSERT INTO players (name, phone, nickname_aliases, base_skill_rating, base_fitness_rating, is_admin, tier)
			VALUES (?, ?, ?, 6.0, 2.0, ?, ?)
		`, p.name, p.phone, p.aliases, p.isAdmin, p.tier)
	}
}

// addTestPlayer adds a player to the test database
func addTestPlayer(name, phone, aliases string) {
	database.DB.Exec(`
		INSERT INTO players (name, phone, nickname_aliases, base_skill_rating, base_fitness_rating, is_admin, tier)
		VALUES (?, ?, ?, 6.0, 2.0, 0, 3)
	`, name, phone, aliases)
}

// makeJSONRequest creates an HTTP request with JSON body
func makeJSONRequest(method, path string, body interface{}) *http.Request {
	var reqBody bytes.Buffer
	if body != nil {
		json.NewEncoder(&reqBody).Encode(body)
	}
	req := httptest.NewRequest(method, path, &reqBody)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// decodeResponse decodes JSON response body
func decodeResponse(w *httptest.ResponseRecorder, v interface{}) {
	err := json.NewDecoder(w.Body).Decode(v)
	Expect(err).NotTo(HaveOccurred())
}

// loginAsPlayer creates a session cookie directly for testing (bypasses HTTP)
func loginAsPlayer(phone string) *http.Cookie {
	// Look up the player ID by phone
	var playerID int
	err := database.DB.QueryRow(`SELECT id FROM players WHERE phone = ?`, phone).Scan(&playerID)
	Expect(err).NotTo(HaveOccurred(), "Player with phone %s not found", phone)

	// Create session token directly
	sessionValue := createSessionToken(playerID)
	return &http.Cookie{
		Name:  "ff_session",
		Value: sessionValue,
	}
}

// submitRating submits a single rating for a player (uses batch endpoint)
func submitRating(cookie *http.Cookie, targetID, skillRating int, fitnessCategory string) {
	req := makeJSONRequest("POST", "/api/ratings", []map[string]interface{}{
		{
			"target_id":        targetID,
			"skill_rating":     skillRating,
			"fitness_category": fitnessCategory,
		},
	})
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	testServer.ServeHTTP(w, req)
	Expect(w.Code).To(Equal(http.StatusOK))
}

// submitBatchRatings submits multiple ratings at once
func submitBatchRatings(cookie *http.Cookie, ratings []map[string]interface{}) *httptest.ResponseRecorder {
	req := makeJSONRequest("POST", "/api/ratings", ratings)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	testServer.ServeHTTP(w, req)
	return w
}

// makePlayerInputs creates player inputs for generate-teams
func makePlayerInputs(count int, useIDs bool) []map[string]interface{} {
	players := make([]map[string]interface{}, count)
	names := []string{"Omer", "Dan", "Niv", "Yossi", "Amit", "Roi", "Gal", "Tomer", "Oren", "Ben", "Lior", "Shai", "Extra1", "Extra2"}
	for i := 0; i < count; i++ {
		if useIDs && i < 12 {
			players[i] = map[string]interface{}{"id": i + 1}
		} else {
			players[i] = map[string]interface{}{"name": names[i]}
		}
	}
	return players
}

// getAllPlayersFromResponse extracts all players from generate-teams response
func getAllPlayersFromResponse(resp map[string]interface{}) []map[string]interface{} {
	home := resp["home"].(map[string]interface{})["players"].([]interface{})
	away := resp["away"].(map[string]interface{})["players"].([]interface{})

	all := make([]map[string]interface{}, 0, len(home)+len(away))
	for _, p := range home {
		all = append(all, p.(map[string]interface{}))
	}
	for _, p := range away {
		all = append(all, p.(map[string]interface{}))
	}
	return all
}

var _ = Describe("HTTP Endpoints", func() {
	Describe("GET /health", func() {
		It("returns ok status", func() {
			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()
			testServer.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp map[string]string
			decodeResponse(w, &resp)
			Expect(resp["status"]).To(Equal("ok"))
		})
	})

	Describe("Unknown endpoints", func() {
		It("returns 404 for unknown API path", func() {
			req := httptest.NewRequest("GET", "/api/unknown", nil)
			w := httptest.NewRecorder()
			testServer.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 405 for wrong HTTP method on API endpoints", func() {
			// Test POST endpoint with DELETE
			req := httptest.NewRequest("DELETE", "/api/auth/google", nil)
			w := httptest.NewRecorder()
			testServer.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusMethodNotAllowed))
		})
	})

	Describe("POST /api/auth/google", func() {
		// Note: Full Google OAuth tests would require mocking the token verification.
		// These tests verify the endpoint exists and handles basic validation.

		When("request is invalid", func() {
			It("returns bad request for empty id_token", func() {
				req := makeJSONRequest("POST", "/api/auth/google", map[string]string{
					"id_token": "",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})

			It("returns bad request for missing body", func() {
				req := httptest.NewRequest("POST", "/api/auth/google", nil)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})
		})
	})

	Describe("POST /api/auth/claim", func() {
		When("request is invalid", func() {
			It("returns bad request for empty claim_token", func() {
				req := makeJSONRequest("POST", "/api/auth/claim", map[string]string{
					"claim_token": "",
					"phone":       "0501234567",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("claim_token"))
			})

			It("returns bad request for empty phone", func() {
				req := makeJSONRequest("POST", "/api/auth/claim", map[string]string{
					"claim_token": "some-token",
					"phone":       "",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("phone"))
			})

			It("returns bad request for missing body", func() {
				req := httptest.NewRequest("POST", "/api/auth/claim", nil)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})

			It("returns unauthorized for invalid claim token", func() {
				req := makeJSONRequest("POST", "/api/auth/claim", map[string]string{
					"claim_token": "invalid-token-that-doesnt-exist",
					"phone":       "0501234567",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusUnauthorized))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("Invalid or expired"))
			})
		})

		When("claim token is valid", func() {
			var validToken string

			BeforeEach(func() {
				// Manually inject a valid claim token for testing
				validToken = generateClaimToken()
				claimTokens[validToken] = claimTokenData{
					Email:     "test@example.com",
					Name:      "Test User",
					ExpiresAt: time.Now().Add(10 * time.Minute),
				}
			})

			AfterEach(func() {
				// Clean up claim tokens
				delete(claimTokens, validToken)
			})

			It("successfully claims account with valid phone", func() {
				req := makeJSONRequest("POST", "/api/auth/claim", map[string]string{
					"claim_token": validToken,
					"phone":       "+972501111111", // Omer's phone
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))
				var resp map[string]interface{}
				decodeResponse(w, &resp)
				Expect(resp["status"]).To(Equal("claimed"))
				Expect(resp["name"]).To(Equal("Omer"))
				Expect(resp["player_id"]).To(BeEquivalentTo(1))

				// Verify session cookie is set
				cookies := w.Result().Cookies()
				var sessionCookie *http.Cookie
				for _, c := range cookies {
					if c.Name == "ff_session" {
						sessionCookie = c
						break
					}
				}
				Expect(sessionCookie).NotTo(BeNil())
				Expect(sessionCookie.Value).NotTo(BeEmpty())

				// Verify email was saved
				player, err := getPlayerByEmail("test@example.com")
				Expect(err).NotTo(HaveOccurred())
				Expect(player.Name).To(Equal("Omer"))
			})

			It("normalizes phone number with hyphens", func() {
				req := makeJSONRequest("POST", "/api/auth/claim", map[string]string{
					"claim_token": validToken,
					"phone":       "+972-501-111-111",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))
				var resp map[string]interface{}
				decodeResponse(w, &resp)
				Expect(resp["status"]).To(Equal("claimed"))
			})

			It("returns not found for unknown phone", func() {
				req := makeJSONRequest("POST", "/api/auth/claim", map[string]string{
					"claim_token": validToken,
					"phone":       "+972599999999", // Non-existent phone
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusNotFound))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("No player found"))
			})

			It("prevents account hijacking when email already linked", func() {
				// First, link an email to Omer's account
				_, err := database.DB.Exec(`UPDATE players SET email = 'existing@example.com' WHERE phone = '+972501111111'`)
				Expect(err).NotTo(HaveOccurred())

				// Now try to claim with a different Google account
				req := makeJSONRequest("POST", "/api/auth/claim", map[string]string{
					"claim_token": validToken,
					"phone":       "+972501111111",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusConflict))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("already linked"))
			})

			It("invalidates claim token after use (one-time use)", func() {
				// First claim should succeed
				req := makeJSONRequest("POST", "/api/auth/claim", map[string]string{
					"claim_token": validToken,
					"phone":       "+972502222222", // Dan's phone (different player)
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)
				Expect(w.Code).To(Equal(http.StatusOK))

				// Second claim with same token should fail
				req2 := makeJSONRequest("POST", "/api/auth/claim", map[string]string{
					"claim_token": validToken,
					"phone":       "+972503333333", // Niv's phone
				})
				w2 := httptest.NewRecorder()
				testServer.ServeHTTP(w2, req2)

				Expect(w2.Code).To(Equal(http.StatusUnauthorized))
				var resp map[string]string
				decodeResponse(w2, &resp)
				Expect(resp["error"]).To(ContainSubstring("Invalid or expired"))
			})
		})

		When("claim token is expired", func() {
			It("rejects expired claim token", func() {
				expiredToken := generateClaimToken()
				claimTokens[expiredToken] = claimTokenData{
					Email:     "expired@example.com",
					Name:      "Expired User",
					ExpiresAt: time.Now().Add(-1 * time.Minute), // Already expired
				}
				defer delete(claimTokens, expiredToken)

				req := makeJSONRequest("POST", "/api/auth/claim", map[string]string{
					"claim_token": expiredToken,
					"phone":       "+972501111111",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusUnauthorized))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("expired"))
			})
		})
	})

	Describe("GET /api/players", func() {
		var sessionCookie *http.Cookie

		BeforeEach(func() {
			sessionCookie = loginAsPlayer("+972501111111")
		})

		When("authenticated", func() {
			It("returns all players except self", func() {
				req := httptest.NewRequest("GET", "/api/players", nil)
				req.AddCookie(sessionCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))

				var players []map[string]interface{}
				decodeResponse(w, &players)
				Expect(players).To(HaveLen(12)) // 13 total players minus self

				for _, p := range players {
					Expect(p["name"]).NotTo(Equal("Omer"))
				}
			})

			It("returns players sorted by tier ASC then name ASC", func() {
				req := httptest.NewRequest("GET", "/api/players", nil)
				req.AddCookie(sessionCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				var players []map[string]interface{}
				decodeResponse(w, &players)

				// Verify tier ordering: lower tiers come first
				for i := 1; i < len(players); i++ {
					prevTier := int(players[i-1]["tier"].(float64))
					currTier := int(players[i]["tier"].(float64))
					if prevTier == currTier {
						// Within same tier, sorted by name
						Expect(players[i-1]["name"].(string) <= players[i]["name"].(string)).To(BeTrue())
					} else {
						// Lower tier comes first
						Expect(prevTier).To(BeNumerically("<", currTier))
					}
				}

				// First players should be tier 1 (Core)
				Expect(players[0]["tier"]).To(BeEquivalentTo(1))
			})

			It("includes tier in response", func() {
				req := httptest.NewRequest("GET", "/api/players", nil)
				req.AddCookie(sessionCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				var players []map[string]interface{}
				decodeResponse(w, &players)

				for _, p := range players {
					Expect(p).To(HaveKey("tier"))
					tier := int(p["tier"].(float64))
					Expect(tier).To(BeNumerically(">=", 1))
					Expect(tier).To(BeNumerically("<=", 4))
				}
			})

			It("includes voter's existing ratings when present", func() {
				submitRating(sessionCookie, 2, 8, "Good")

				req := httptest.NewRequest("GET", "/api/players", nil)
				req.AddCookie(sessionCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				var players []map[string]interface{}
				decodeResponse(w, &players)

				var dan map[string]interface{}
				for _, p := range players {
					if p["name"] == "Dan" {
						dan = p
						break
					}
				}
				Expect(dan["my_skill_rating"]).To(BeEquivalentTo(8))
				Expect(dan["my_fitness_rating"]).To(Equal("Good"))
			})

			It("omits my_* fields when player not rated", func() {
				req := httptest.NewRequest("GET", "/api/players", nil)
				req.AddCookie(sessionCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				var players []map[string]interface{}
				decodeResponse(w, &players)

				for _, p := range players {
					_, hasSkill := p["my_skill_rating"]
					_, hasFitness := p["my_fitness_rating"]
					Expect(hasSkill).To(BeFalse())
					Expect(hasFitness).To(BeFalse())
				}
			})
		})

		When("not authenticated", func() {
			It("returns unauthorized without cookie", func() {
				req := httptest.NewRequest("GET", "/api/players", nil)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusUnauthorized))
			})

			It("returns unauthorized with invalid cookie", func() {
				req := httptest.NewRequest("GET", "/api/players", nil)
				req.AddCookie(&http.Cookie{Name: "ff_session", Value: "invalid"})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusUnauthorized))
			})

			It("returns unauthorized with tampered cookie", func() {
				req := httptest.NewRequest("GET", "/api/players", nil)
				req.AddCookie(&http.Cookie{Name: "ff_session", Value: "MToxMjM0NS50YW1wZXJlZA=="})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusUnauthorized))
			})
		})
	})

	Describe("POST /api/ratings", func() {
		var sessionCookie *http.Cookie

		BeforeEach(func() {
			sessionCookie = loginAsPlayer("+972501111111")
		})

		When("submitting valid batch ratings", func() {
			It("saves successfully and returns saved_count", func() {
				ratings := []map[string]interface{}{
					{"target_id": 2, "skill_rating": 7, "fitness_category": "Good"},
					{"target_id": 3, "skill_rating": 8, "fitness_category": "Great"},
				}
				w := submitBatchRatings(sessionCookie, ratings)

				Expect(w.Code).To(Equal(http.StatusOK))
				var resp map[string]interface{}
				decodeResponse(w, &resp)
				Expect(resp["success"]).To(BeTrue())
				Expect(resp["saved_count"]).To(BeEquivalentTo(2))
			})

			It("accepts empty array", func() {
				w := submitBatchRatings(sessionCookie, []map[string]interface{}{})

				Expect(w.Code).To(Equal(http.StatusOK))
				var resp map[string]interface{}
				decodeResponse(w, &resp)
				Expect(resp["success"]).To(BeTrue())
				Expect(resp["saved_count"]).To(BeEquivalentTo(0))
			})

			It("accepts all fitness categories", func() {
				ratings := []map[string]interface{}{
					{"target_id": 2, "skill_rating": 5, "fitness_category": "Low"},
					{"target_id": 3, "skill_rating": 5, "fitness_category": "Ok"},
					{"target_id": 4, "skill_rating": 5, "fitness_category": "GOOD"},
					{"target_id": 5, "skill_rating": 5, "fitness_category": "great"},
					{"target_id": 6, "skill_rating": 5, "fitness_category": "Excellent"},
				}
				w := submitBatchRatings(sessionCookie, ratings)

				Expect(w.Code).To(Equal(http.StatusOK))
			})

			It("updates existing ratings (upsert)", func() {
				submitRating(sessionCookie, 2, 5, "Ok")
				submitRating(sessionCookie, 2, 9, "Great")

				req := httptest.NewRequest("GET", "/api/players", nil)
				req.AddCookie(sessionCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				var players []map[string]interface{}
				decodeResponse(w, &players)

				var dan map[string]interface{}
				for _, p := range players {
					if p["name"] == "Dan" {
						dan = p
						break
					}
				}
				Expect(dan["my_skill_rating"]).To(BeEquivalentTo(9))
				Expect(dan["my_fitness_rating"]).To(Equal("Great"))
			})

			It("persists all ratings after GET /api/players", func() {
				ratings := []map[string]interface{}{
					{"target_id": 2, "skill_rating": 7, "fitness_category": "Great"},
					{"target_id": 3, "skill_rating": 8, "fitness_category": "Ok"},
					{"target_id": 4, "skill_rating": 6, "fitness_category": "Good"},
				}
				submitBatchRatings(sessionCookie, ratings)

				req := httptest.NewRequest("GET", "/api/players", nil)
				req.AddCookie(sessionCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				var players []map[string]interface{}
				decodeResponse(w, &players)

				ratedCount := 0
				for _, p := range players {
					if _, ok := p["my_skill_rating"]; ok {
						ratedCount++
					}
				}
				Expect(ratedCount).To(Equal(3))
			})
		})

		When("batch contains invalid rating", func() {
			It("rejects self-rating", func() {
				ratings := []map[string]interface{}{
					{"target_id": 2, "skill_rating": 7, "fitness_category": "Good"},
					{"target_id": 1, "skill_rating": 10, "fitness_category": "Great"}, // Self-rating
				}
				w := submitBatchRatings(sessionCookie, ratings)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("rate yourself"))
			})

			It("rejects invalid target_id", func() {
				ratings := []map[string]interface{}{
					{"target_id": 0, "skill_rating": 7, "fitness_category": "Good"},
				}
				w := submitBatchRatings(sessionCookie, ratings)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("invalid target_id"))
			})

			It("rejects skill_rating out of range", func() {
				ratings := []map[string]interface{}{
					{"target_id": 2, "skill_rating": 11, "fitness_category": "Good"},
				}
				w := submitBatchRatings(sessionCookie, ratings)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("skill_rating must be between 1 and 10"))
			})

			It("rejects invalid fitness_category", func() {
				ratings := []map[string]interface{}{
					{"target_id": 2, "skill_rating": 7, "fitness_category": "Super"},
				}
				w := submitBatchRatings(sessionCookie, ratings)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("Low"))
			})

			It("includes index in error message", func() {
				ratings := []map[string]interface{}{
					{"target_id": 2, "skill_rating": 7, "fitness_category": "Good"},
					{"target_id": 3, "skill_rating": 15, "fitness_category": "Great"}, // Invalid at index 1
				}
				w := submitBatchRatings(sessionCookie, ratings)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("rating 1"))
			})
		})

		When("request is malformed", func() {
			It("rejects non-array body", func() {
				req := makeJSONRequest("POST", "/api/ratings", map[string]interface{}{
					"target_id": 2, "skill_rating": 7, "fitness_category": "Good",
				})
				req.AddCookie(sessionCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})
		})

		When("not authenticated", func() {
			It("returns unauthorized", func() {
				ratings := []map[string]interface{}{
					{"target_id": 2, "skill_rating": 7, "fitness_category": "Good"},
				}
				req := makeJSONRequest("POST", "/api/ratings", ratings)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusUnauthorized))
			})
		})
	})

	Describe("POST /api/admin/generate-teams", func() {
		var adminCookie *http.Cookie

		BeforeEach(func() {
			adminCookie = loginAsPlayer("+972509090909") // Admin user
		})

		When("admin gives valid players by ID", func() {
			It("generates balanced teams with 6 players each", func() {
				players := makePlayerInputs(12, true)

				req := makeJSONRequest("POST", "/api/admin/generate-teams", map[string]interface{}{
					"players":          players,
					"consider_fitness": true,
				})
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))

				var resp map[string]interface{}
				decodeResponse(w, &resp)

				home := resp["home"].(map[string]interface{})
				away := resp["away"].(map[string]interface{})
				Expect(home["players"]).To(HaveLen(6))
				Expect(away["players"]).To(HaveLen(6))
				Expect(resp).To(HaveKey("skill_delta"))
				Expect(resp).To(HaveKey("fitness_delta"))
				Expect(resp).To(HaveKey("total_cost"))
			})
		})

		When("admin gives valid players by name", func() {
			It("resolves names and generates teams", func() {
				players := []map[string]interface{}{
					{"name": "Omer"}, {"name": "Dan"}, {"name": "Niv"},
					{"name": "Yossi"}, {"name": "Amit"}, {"name": "Roi"},
					{"name": "Gal"}, {"name": "Tomer"}, {"name": "Oren"},
					{"name": "Ben"}, {"name": "Lior"}, {"name": "Shai"},
				}

				req := makeJSONRequest("POST", "/api/admin/generate-teams", map[string]interface{}{
					"players":          players,
					"consider_fitness": false,
				})
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))
			})

			It("resolves nickname aliases", func() {
				players := []map[string]interface{}{
					{"name": "Omeri"}, {"name": "Danny"}, {"name": "Nivi"},
					{"name": "Yossi"}, {"name": "Amit"}, {"name": "Roi"},
					{"name": "Gal"}, {"name": "Tomer"}, {"name": "Oren"},
					{"name": "Ben"}, {"name": "Lior"}, {"name": "Shai"},
				}

				req := makeJSONRequest("POST", "/api/admin/generate-teams", map[string]interface{}{
					"players":          players,
					"consider_fitness": false,
				})
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))

				var resp map[string]interface{}
				decodeResponse(w, &resp)
				allPlayers := getAllPlayersFromResponse(resp)

				names := make([]string, len(allPlayers))
				for i, p := range allPlayers {
					names[i] = p["name"].(string)
				}
				Expect(names).To(ContainElement("Omer"))
				Expect(names).To(ContainElement("Dan"))
			})

			It("resolves case-insensitively", func() {
				players := []map[string]interface{}{
					{"name": "OMER"}, {"name": "dan"}, {"name": "NiV"},
					{"name": "yossi"}, {"name": "AMIT"}, {"name": "roi"},
					{"name": "GAL"}, {"name": "tomer"}, {"name": "OREN"},
					{"name": "ben"}, {"name": "LIOR"}, {"name": "shai"},
				}

				req := makeJSONRequest("POST", "/api/admin/generate-teams", map[string]interface{}{
					"players":          players,
					"consider_fitness": false,
				})
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))
			})
		})

		When("admin gives mix of IDs and names", func() {
			It("resolves both", func() {
				players := []map[string]interface{}{
					{"id": 1}, {"id": 2}, {"name": "Niv"},
					{"id": 4}, {"name": "Amit"}, {"id": 6},
					{"id": 7}, {"id": 8}, {"name": "Oren"},
					{"id": 10}, {"id": 11}, {"name": "Shai"},
				}

				req := makeJSONRequest("POST", "/api/admin/generate-teams", map[string]interface{}{
					"players":          players,
					"consider_fitness": true,
				})
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))
			})
		})

		When("admin gives guest players (unknown names)", func() {
			It("creates guests with default ratings", func() {
				players := makePlayerInputs(11, true)
				players = append(players, map[string]interface{}{"name": "David's Friend"})

				req := makeJSONRequest("POST", "/api/admin/generate-teams", map[string]interface{}{
					"players":          players,
					"consider_fitness": true,
				})
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))

				var resp map[string]interface{}
				decodeResponse(w, &resp)
				allPlayers := getAllPlayersFromResponse(resp)

				var guest map[string]interface{}
				for _, p := range allPlayers {
					if p["name"] == "David's Friend" {
						guest = p
						break
					}
				}
				Expect(guest).NotTo(BeNil())
				Expect(guest["id"]).To(BeEquivalentTo(-1))
				Expect(guest["base_skill_rating"]).To(BeEquivalentTo(5.0))
				Expect(guest["base_fitness_rating"]).To(BeEquivalentTo(3.0))
			})

			It("supports multiple guests", func() {
				players := makePlayerInputs(10, true)
				players = append(players,
					map[string]interface{}{"name": "Guest 1"},
					map[string]interface{}{"name": "Guest 2"},
				)

				req := makeJSONRequest("POST", "/api/admin/generate-teams", map[string]interface{}{
					"players":          players,
					"consider_fitness": true,
				})
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))
			})
		})

		When("using peer ratings", func() {
			It("uses average ratings from anonymous_ratings", func() {
				database.DB.Exec(`
					INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating)
					VALUES (1, 2, 10, 3), (3, 2, 10, 3)
				`)

				players := makePlayerInputs(12, true)

				req := makeJSONRequest("POST", "/api/admin/generate-teams", map[string]interface{}{
					"players":          players,
					"consider_fitness": true,
				})
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))
			})

			It("falls back to base rating when no peer ratings", func() {
				players := makePlayerInputs(12, true)

				req := makeJSONRequest("POST", "/api/admin/generate-teams", map[string]interface{}{
					"players":          players,
					"consider_fitness": true,
				})
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))
			})
		})

		When("request is invalid", func() {
			It("rejects wrong player count", func() {
				for _, count := range []int{0, 10, 14} {
					players := makePlayerInputs(count, true)

					req := makeJSONRequest("POST", "/api/admin/generate-teams", map[string]interface{}{
						"players":          players,
						"consider_fitness": true,
					})
					req.AddCookie(adminCookie)
					w := httptest.NewRecorder()
					testServer.ServeHTTP(w, req)

					Expect(w.Code).To(Equal(http.StatusBadRequest))
				}
			})

			It("rejects non-existent player ID", func() {
				players := makePlayerInputs(11, true)
				players = append(players, map[string]interface{}{"id": 999})

				req := makeJSONRequest("POST", "/api/admin/generate-teams", map[string]interface{}{
					"players":          players,
					"consider_fitness": true,
				})
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})

			It("rejects player with neither id nor name", func() {
				players := makePlayerInputs(11, true)
				players = append(players, map[string]interface{}{})

				req := makeJSONRequest("POST", "/api/admin/generate-teams", map[string]interface{}{
					"players":          players,
					"consider_fitness": true,
				})
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})

			It("rejects invalid JSON", func() {
				req := httptest.NewRequest("POST", "/api/admin/generate-teams", nil)
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})
		})

		When("not authenticated", func() {
			It("returns unauthorized without session", func() {
				players := makePlayerInputs(12, true)

				req := makeJSONRequest("POST", "/api/admin/generate-teams", map[string]interface{}{
					"players":          players,
					"consider_fitness": true,
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusUnauthorized))
			})
		})

		When("authenticated as non-admin", func() {
			It("returns forbidden", func() {
				regularCookie := loginAsPlayer("+972501111111") // Non-admin user
				players := makePlayerInputs(12, true)

				req := makeJSONRequest("POST", "/api/admin/generate-teams", map[string]interface{}{
					"players":          players,
					"consider_fitness": true,
				})
				req.AddCookie(regularCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusForbidden))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("Only the manager"))
			})
		})
	})

	Describe("POST /api/admin/recalculate-base-ratings", func() {
		var adminCookie *http.Cookie

		BeforeEach(func() {
			adminCookie = loginAsPlayer("+972509090909") // Admin user
		})

		When("admin triggers recalculation", func() {
			It("updates players with ratings and returns count", func() {
				// Add some ratings
				database.DB.Exec(`INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating) VALUES
					(1, 2, 8, 3),
					(3, 2, 10, 3),
					(1, 3, 4, 1)`)

				req := httptest.NewRequest("POST", "/api/admin/recalculate-base-ratings", nil)
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))

				var resp map[string]interface{}
				decodeResponse(w, &resp)
				Expect(resp["success"]).To(BeTrue())
				Expect(resp["players_updated"]).To(BeEquivalentTo(2)) // Dan and Niv have ratings
			})

			It("returns 0 when no ratings exist", func() {
				req := httptest.NewRequest("POST", "/api/admin/recalculate-base-ratings", nil)
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))

				var resp map[string]interface{}
				decodeResponse(w, &resp)
				Expect(resp["success"]).To(BeTrue())
				Expect(resp["players_updated"]).To(BeEquivalentTo(0))
			})

			It("correctly computes average ratings", func() {
				// Add multiple ratings for Dan (id=2)
				database.DB.Exec(`INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating) VALUES
					(1, 2, 6, 1),
					(3, 2, 8, 3),
					(4, 2, 10, 2)`)

				req := httptest.NewRequest("POST", "/api/admin/recalculate-base-ratings", nil)
				req.AddCookie(adminCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))

				// Verify Dan's ratings were updated correctly
				players, _ := database.GetPlayersByIDs([]int{2})
				Expect(players).To(HaveLen(1))
				// Skill: (6+8+10)/3 = 8.0
				Expect(players[0].BaseSkillRating).To(Equal(8.0))
				// Fitness: (1+3+2)/3 = 2.0
				Expect(players[0].BaseFitnessRating).To(Equal(2.0))
			})
		})

		When("not authenticated", func() {
			It("returns unauthorized", func() {
				req := httptest.NewRequest("POST", "/api/admin/recalculate-base-ratings", nil)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusUnauthorized))
			})
		})

		When("authenticated as non-admin", func() {
			It("returns forbidden", func() {
				regularCookie := loginAsPlayer("+972501111111") // Non-admin user

				req := httptest.NewRequest("POST", "/api/admin/recalculate-base-ratings", nil)
				req.AddCookie(regularCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusForbidden))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("Only the manager"))
			})
		})
	})
})
