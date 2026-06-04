package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"

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
		{"Omer", "+972501111111", `["Omeri"]`, 0, 1},  // Core
		{"Dan", "+972502222222", `["Danny"]`, 0, 1},   // Core
		{"Niv", "+972503333333", `["Nivi"]`, 0, 2},    // Regular
		{"Yossi", "+972504444444", `[]`, 0, 2},        // Regular
		{"Amit", "+972505555555", `[]`, 0, 2},         // Regular
		{"Roi", "+972506666666", `[]`, 0, 3},          // Occasional
		{"Gal", "+972507777777", `[]`, 0, 3},          // Occasional
		{"Tomer", "+972508888888", `[]`, 0, 3},        // Occasional
		{"Oren", "+972509999999", `[]`, 0, 3},         // Occasional
		{"Ben", "+972500000000", `[]`, 0, 4},          // Rare
		{"Lior", "+972501010101", `[]`, 0, 4},         // Rare
		{"Shai", "+972502020202", `[]`, 0, 4},         // Rare
		{"Admin", "+972509090909", `[]`, 1, 1},        // Admin (Core)
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

// loginAsPlayer logs in and returns the session cookie
func loginAsPlayer(phone string) *http.Cookie {
	req := makeJSONRequest("POST", "/api/auth/login", map[string]string{
		"phone": phone,
	})
	w := httptest.NewRecorder()
	testServer.ServeHTTP(w, req)
	Expect(w.Code).To(Equal(http.StatusOK))
	cookies := w.Result().Cookies()
	Expect(cookies).To(HaveLen(1))
	return cookies[0]
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
			// Test POST endpoint with GET
			req := httptest.NewRequest("DELETE", "/api/auth/login", nil)
			w := httptest.NewRecorder()
			testServer.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusMethodNotAllowed))
		})
	})

	Describe("POST /api/auth/login", func() {
		When("phone number is valid and exists", func() {
			It("returns player info and sets session cookie", func() {
				req := makeJSONRequest("POST", "/api/auth/login", map[string]string{
					"phone": "+972501111111",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))

				var resp map[string]interface{}
				decodeResponse(w, &resp)
				Expect(resp["name"]).To(Equal("Omer"))
				Expect(resp["player_id"]).To(BeNumerically(">", 0))
				Expect(resp["is_admin"]).To(BeFalse())

				cookies := w.Result().Cookies()
				Expect(cookies).To(HaveLen(1))
				Expect(cookies[0].Name).To(Equal("ff_session"))
			})

			It("returns is_admin true for admin users", func() {
				req := makeJSONRequest("POST", "/api/auth/login", map[string]string{
					"phone": "+972509090909",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))

				var resp map[string]interface{}
				decodeResponse(w, &resp)
				Expect(resp["name"]).To(Equal("Admin"))
				Expect(resp["is_admin"]).To(BeTrue())
			})
		})

		When("phone number has dashes", func() {
			It("normalizes and finds the player", func() {
				req := makeJSONRequest("POST", "/api/auth/login", map[string]string{
					"phone": "+972-50-111-1111",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))
				var resp map[string]interface{}
				decodeResponse(w, &resp)
				Expect(resp["name"]).To(Equal("Omer"))
			})
		})

		When("phone number is local format (10 digits)", func() {
			It("finds the player", func() {
				addTestPlayer("Local", "0501234567", `[]`)

				req := makeJSONRequest("POST", "/api/auth/login", map[string]string{
					"phone": "0501234567",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusOK))
				var resp map[string]interface{}
				decodeResponse(w, &resp)
				Expect(resp["name"]).To(Equal("Local"))
			})
		})

		When("phone number is invalid length", func() {
			It("returns bad request for too short", func() {
				req := makeJSONRequest("POST", "/api/auth/login", map[string]string{
					"phone": "12345",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("10 digits"))
			})

			It("returns bad request for too long", func() {
				req := makeJSONRequest("POST", "/api/auth/login", map[string]string{
					"phone": "+97250123456789999",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})
		})

		When("phone number does not exist", func() {
			It("returns unauthorized", func() {
				req := makeJSONRequest("POST", "/api/auth/login", map[string]string{
					"phone": "+972599999999",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusUnauthorized))
			})
		})

		When("request is invalid", func() {
			It("returns bad request for empty phone", func() {
				req := makeJSONRequest("POST", "/api/auth/login", map[string]string{
					"phone": "",
				})
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
			})

			It("returns bad request for invalid JSON", func() {
				req := httptest.NewRequest("POST", "/api/auth/login", nil)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
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

			It("includes base ratings", func() {
				req := httptest.NewRequest("GET", "/api/players", nil)
				req.AddCookie(sessionCookie)
				w := httptest.NewRecorder()
				testServer.ServeHTTP(w, req)

				var players []map[string]interface{}
				decodeResponse(w, &players)

				for _, p := range players {
					Expect(p).To(HaveKey("base_skill_rating"))
					Expect(p).To(HaveKey("base_fitness_rating"))
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
					{"target_id": 2, "skill_rating": 7, "fitness_category": "Normal"},
					{"target_id": 3, "skill_rating": 8, "fitness_category": "Good"},
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
					{"target_id": 2, "skill_rating": 5, "fitness_category": "Poor"},
					{"target_id": 3, "skill_rating": 5, "fitness_category": "NORMAL"},
					{"target_id": 4, "skill_rating": 5, "fitness_category": "good"},
				}
				w := submitBatchRatings(sessionCookie, ratings)

				Expect(w.Code).To(Equal(http.StatusOK))
			})

			It("updates existing ratings (upsert)", func() {
				submitRating(sessionCookie, 2, 5, "Poor")
				submitRating(sessionCookie, 2, 9, "Good")

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
				Expect(dan["my_fitness_rating"]).To(Equal("Good"))
			})

			It("persists all ratings after GET /api/players", func() {
				ratings := []map[string]interface{}{
					{"target_id": 2, "skill_rating": 7, "fitness_category": "Good"},
					{"target_id": 3, "skill_rating": 8, "fitness_category": "Poor"},
					{"target_id": 4, "skill_rating": 6, "fitness_category": "Normal"},
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
					{"target_id": 2, "skill_rating": 7, "fitness_category": "Normal"},
					{"target_id": 1, "skill_rating": 10, "fitness_category": "Good"}, // Self-rating
				}
				w := submitBatchRatings(sessionCookie, ratings)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("cannot rate yourself"))
			})

			It("rejects invalid target_id", func() {
				ratings := []map[string]interface{}{
					{"target_id": 0, "skill_rating": 7, "fitness_category": "Normal"},
				}
				w := submitBatchRatings(sessionCookie, ratings)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("invalid target_id"))
			})

			It("rejects skill_rating out of range", func() {
				ratings := []map[string]interface{}{
					{"target_id": 2, "skill_rating": 11, "fitness_category": "Normal"},
				}
				w := submitBatchRatings(sessionCookie, ratings)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("skill_rating must be between 1 and 10"))
			})

			It("rejects invalid fitness_category", func() {
				ratings := []map[string]interface{}{
					{"target_id": 2, "skill_rating": 7, "fitness_category": "Excellent"},
				}
				w := submitBatchRatings(sessionCookie, ratings)

				Expect(w.Code).To(Equal(http.StatusBadRequest))
				var resp map[string]string
				decodeResponse(w, &resp)
				Expect(resp["error"]).To(ContainSubstring("Poor"))
			})

			It("includes index in error message", func() {
				ratings := []map[string]interface{}{
					{"target_id": 2, "skill_rating": 7, "fitness_category": "Normal"},
					{"target_id": 3, "skill_rating": 15, "fitness_category": "Good"}, // Invalid at index 1
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
					"target_id": 2, "skill_rating": 7, "fitness_category": "Normal",
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
					{"target_id": 2, "skill_rating": 7, "fitness_category": "Normal"},
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
				Expect(guest["base_fitness_rating"]).To(BeEquivalentTo(2.0))
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
				Expect(resp["error"]).To(ContainSubstring("admin access required"))
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
				Expect(resp["error"]).To(ContainSubstring("admin access required"))
			})
		})
	})
})
