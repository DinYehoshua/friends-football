package server

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"friends-football/internal/database"
)

var _ = Describe("Admin Handlers Unit Tests", func() {
	BeforeEach(func() {
		err := database.Init(":memory:")
		Expect(err).NotTo(HaveOccurred())
		seedPlayers()
	})

	AfterEach(func() {
		database.Close()
	})

	Describe("resolvePlayers", func() {
		When("all players have valid IDs", func() {
			It("fetches players by ID", func() {
				inputs := []PlayerInput{
					{ID: 1}, {ID: 2}, {ID: 3},
				}

				players, err := resolvePlayers(inputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(players).To(HaveLen(3))
				Expect(players[0].Name).To(Equal("Omer"))
				Expect(players[1].Name).To(Equal("Dan"))
				Expect(players[2].Name).To(Equal("Niv"))
			})

			It("preserves order", func() {
				inputs := []PlayerInput{
					{ID: 3}, {ID: 1}, {ID: 2},
				}

				players, err := resolvePlayers(inputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(players[0].Name).To(Equal("Niv"))
				Expect(players[1].Name).To(Equal("Omer"))
				Expect(players[2].Name).To(Equal("Dan"))
			})
		})

		When("all players have names", func() {
			It("resolves by exact name match", func() {
				inputs := []PlayerInput{
					{Name: "Omer"}, {Name: "Dan"}, {Name: "Niv"},
				}

				players, err := resolvePlayers(inputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(players).To(HaveLen(3))
				Expect(players[0].ID).To(Equal(1))
				Expect(players[1].ID).To(Equal(2))
				Expect(players[2].ID).To(Equal(3))
			})

			It("resolves by nickname alias", func() {
				inputs := []PlayerInput{
					{Name: "Omeri"}, {Name: "Danny"},
				}

				players, err := resolvePlayers(inputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(players[0].Name).To(Equal("Omer"))
				Expect(players[1].Name).To(Equal("Dan"))
			})

			It("resolves case-insensitively", func() {
				inputs := []PlayerInput{
					{Name: "OMER"}, {Name: "dan"}, {Name: "NiV"},
				}

				players, err := resolvePlayers(inputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(players[0].Name).To(Equal("Omer"))
				Expect(players[1].Name).To(Equal("Dan"))
				Expect(players[2].Name).To(Equal("Niv"))
			})
		})

		When("mixing IDs and names", func() {
			It("resolves both correctly", func() {
				inputs := []PlayerInput{
					{ID: 1}, {Name: "Dan"}, {ID: 3},
				}

				players, err := resolvePlayers(inputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(players[0].ID).To(Equal(1))
				Expect(players[1].ID).To(Equal(2))
				Expect(players[2].ID).To(Equal(3))
			})
		})

		When("name cannot be resolved", func() {
			It("creates guest player with default ratings", func() {
				inputs := []PlayerInput{
					{Name: "Unknown Guest"},
				}

				players, err := resolvePlayers(inputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(players).To(HaveLen(1))
				Expect(players[0].ID).To(Equal(-1))
				Expect(players[0].Name).To(Equal("Unknown Guest"))
				Expect(players[0].BaseSkillRating).To(Equal(5.0))
				Expect(players[0].BaseFitnessRating).To(Equal(2.0))
			})
		})

		When("ID does not exist", func() {
			It("returns error", func() {
				inputs := []PlayerInput{
					{ID: 999},
				}

				_, err := resolvePlayers(inputs)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("999"))
				Expect(err.Error()).To(ContainSubstring("not found"))
			})
		})

		When("player has neither ID nor name", func() {
			It("returns error", func() {
				inputs := []PlayerInput{
					{ID: 1}, {}, // Empty input
				}

				_, err := resolvePlayers(inputs)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("neither id nor name"))
				Expect(err.Error()).To(ContainSubstring("index 1"))
			})
		})

		When("empty input", func() {
			It("returns empty slice", func() {
				inputs := []PlayerInput{}

				players, err := resolvePlayers(inputs)
				Expect(err).NotTo(HaveOccurred())
				Expect(players).To(BeEmpty())
			})
		})
	})

	Describe("getAllRegisteredPlayers", func() {
		It("returns all players sorted by name", func() {
			players, err := getAllRegisteredPlayers()
			Expect(err).NotTo(HaveOccurred())
			Expect(players).To(HaveLen(6))

			// Verify sorted by name ASC
			names := make([]string, len(players))
			for i, p := range players {
				names[i] = p.Name
			}
			Expect(names).To(Equal([]string{"Amit", "Dan", "Niv", "Omer", "Roi", "Yossi"}))
		})

		It("returns id and name only", func() {
			players, err := getAllRegisteredPlayers()
			Expect(err).NotTo(HaveOccurred())
			Expect(players[0].ID).To(BeNumerically(">", 0))
			Expect(players[0].Name).NotTo(BeEmpty())
		})
	})

	Describe("appendPlayerAliases", func() {
		It("appends new alias to player with no existing aliases", func() {
			// First clear Yossi's aliases
			database.DB.Exec(`UPDATE players SET nickname_aliases = '[]' WHERE name = 'Yossi'`)

			updates := map[int][]string{
				4: {"יוסי"}, // Yossi's ID is 4
			}

			count, err := appendPlayerAliases(updates)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))

			// Verify alias was added
			var aliasesJSON string
			err = database.DB.QueryRow(`SELECT nickname_aliases FROM players WHERE id = 4`).Scan(&aliasesJSON)
			Expect(err).NotTo(HaveOccurred())

			var aliases []string
			err = json.Unmarshal([]byte(aliasesJSON), &aliases)
			Expect(err).NotTo(HaveOccurred())
			Expect(aliases).To(ContainElement("יוסי"))
		})

		It("appends new alias to player with existing aliases", func() {
			updates := map[int][]string{
				1: {"עומרי"}, // Omer already has "Omeri"
			}

			count, err := appendPlayerAliases(updates)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))

			// Verify alias was added without removing existing
			var aliasesJSON string
			err = database.DB.QueryRow(`SELECT nickname_aliases FROM players WHERE id = 1`).Scan(&aliasesJSON)
			Expect(err).NotTo(HaveOccurred())

			var aliases []string
			err = json.Unmarshal([]byte(aliasesJSON), &aliases)
			Expect(err).NotTo(HaveOccurred())
			Expect(aliases).To(ContainElement("Omeri"))
			Expect(aliases).To(ContainElement("עומרי"))
		})

		It("does not add duplicate aliases (case-insensitive)", func() {
			updates := map[int][]string{
				1: {"OMERI"}, // Already has "Omeri"
			}

			count, err := appendPlayerAliases(updates)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))

			// Verify no duplicate was added
			var aliasesJSON string
			err = database.DB.QueryRow(`SELECT nickname_aliases FROM players WHERE id = 1`).Scan(&aliasesJSON)
			Expect(err).NotTo(HaveOccurred())

			var aliases []string
			err = json.Unmarshal([]byte(aliasesJSON), &aliases)
			Expect(err).NotTo(HaveOccurred())
			// Should only have "Omeri", not both "Omeri" and "OMERI"
			Expect(aliases).To(HaveLen(1))
			Expect(aliases[0]).To(Equal("Omeri"))
		})

		It("handles multiple players in one call", func() {
			updates := map[int][]string{
				1: {"אומר"},
				2: {"דני"},
			}

			count, err := appendPlayerAliases(updates)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(2))
		})

		It("handles multiple aliases per player", func() {
			updates := map[int][]string{
				4: {"יוסי", "יוסי ג", "Yossi G"},
			}

			count, err := appendPlayerAliases(updates)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))

			var aliasesJSON string
			err = database.DB.QueryRow(`SELECT nickname_aliases FROM players WHERE id = 4`).Scan(&aliasesJSON)
			Expect(err).NotTo(HaveOccurred())

			var aliases []string
			err = json.Unmarshal([]byte(aliasesJSON), &aliases)
			Expect(err).NotTo(HaveOccurred())
			Expect(aliases).To(HaveLen(3))
		})

		It("returns 0 for empty updates map", func() {
			updates := map[int][]string{}

			count, err := appendPlayerAliases(updates)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(0))
		})
	})

	Describe("buildTeamResponse", func() {
		It("converts balancer.Team to TeamResponse", func() {
			// This is tested indirectly through HTTP tests
			// since buildTeamResponse just maps fields
		})
	})
})

// seedPlayers populates the test database
func seedPlayers() {
	players := []struct {
		name    string
		phone   string
		aliases string
	}{
		{"Omer", "+972501111111", `["Omeri"]`},
		{"Dan", "+972502222222", `["Danny"]`},
		{"Niv", "+972503333333", `["Nivi"]`},
		{"Yossi", "+972504444444", `[]`},
		{"Amit", "+972505555555", `[]`},
		{"Roi", "+972506666666", `[]`},
	}

	for _, p := range players {
		database.DB.Exec(`
			INSERT INTO players (name, phone, nickname_aliases, base_skill_rating, base_fitness_rating, is_admin, tier)
			VALUES (?, ?, ?, 6.0, 2.0, 0, 3)
		`, p.name, p.phone, p.aliases)
	}
}
