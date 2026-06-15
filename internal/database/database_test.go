package database_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"friends-football/internal/database"
)

func TestDatabase(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Database Suite")
}

var _ = Describe("Database", func() {
	BeforeEach(func() {
		// Use in-memory database for each test
		err := database.Init(":memory:")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		database.Close()
	})

	Context("Init", func() {
		It("creates tables with correct schema", func() {
			// Verify players table exists with correct columns
			_, err := database.DB.Exec(`
				INSERT INTO players (name, phone, base_skill_rating, base_fitness_rating)
				VALUES ('Test', '+1234567890', 7.5, 2.0)
			`)
			Expect(err).NotTo(HaveOccurred())

			// Verify anonymous_ratings table exists with correct constraints
			_, err = database.DB.Exec(`
				INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating)
				VALUES (1, 1, 8, 2)
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("enforces fitness_rating constraint (1-3)", func() {
			database.DB.Exec(`INSERT INTO players (name, phone) VALUES ('A', '+1'), ('B', '+2')`)

			// Valid fitness ratings
			_, err := database.DB.Exec(`INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating) VALUES (1, 2, 5, 1)`)
			Expect(err).NotTo(HaveOccurred())

			_, err = database.DB.Exec(`INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating) VALUES (2, 1, 5, 3)`)
			Expect(err).NotTo(HaveOccurred())

			// Invalid fitness rating (too high)
			_, err = database.DB.Exec(`INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating) VALUES (1, 2, 5, 4)`)
			Expect(err).To(HaveOccurred())

			// Invalid fitness rating (too low)
			_, err = database.DB.Exec(`INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating) VALUES (1, 2, 5, 0)`)
			Expect(err).To(HaveOccurred())
		})

		It("enforces skill_rating constraint (1-10)", func() {
			database.DB.Exec(`INSERT INTO players (name, phone) VALUES ('A', '+1'), ('B', '+2')`)

			// Invalid skill rating (too high)
			_, err := database.DB.Exec(`INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating) VALUES (1, 2, 11, 2)`)
			Expect(err).To(HaveOccurred())

			// Invalid skill rating (too low)
			_, err = database.DB.Exec(`INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating) VALUES (1, 2, 0, 2)`)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("GetPlayersByIDs", func() {
		BeforeEach(func() {
			database.DB.Exec(`INSERT INTO players (id, name, phone, base_skill_rating, base_fitness_rating) VALUES
				(1, 'Alice', '+1111', 8.0, 3.0),
				(2, 'Bob', '+2222', 6.5, 2.0),
				(3, 'Charlie', '+3333', 7.0, 1.0)`)
		})

		It("retrieves players by their IDs", func() {
			players, err := database.GetPlayersByIDs([]int{1, 3})
			Expect(err).NotTo(HaveOccurred())
			Expect(players).To(HaveLen(2))

			names := make(map[string]bool)
			for _, p := range players {
				names[p.Name] = true
			}
			Expect(names).To(HaveKey("Alice"))
			Expect(names).To(HaveKey("Charlie"))
		})

		It("returns empty slice for empty input", func() {
			players, err := database.GetPlayersByIDs([]int{})
			Expect(err).NotTo(HaveOccurred())
			Expect(players).To(BeNil())
		})

		It("returns correct ratings", func() {
			players, err := database.GetPlayersByIDs([]int{1})
			Expect(err).NotTo(HaveOccurred())
			Expect(players).To(HaveLen(1))
			Expect(players[0].BaseSkillRating).To(Equal(8.0))
			Expect(players[0].BaseFitnessRating).To(Equal(3.0))
		})
	})

	Context("ComputeAverageRatings", func() {
		BeforeEach(func() {
			// Create players
			database.DB.Exec(`INSERT INTO players (id, name, phone) VALUES
				(1, 'Alice', '+1111'),
				(2, 'Bob', '+2222'),
				(3, 'Charlie', '+3333'),
				(4, 'Diana', '+4444')`)

			// Add ratings for player 1 (Alice): skill avg=7, fitness avg=2
			database.DB.Exec(`INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating) VALUES
				(2, 1, 6, 1),
				(3, 1, 8, 3),
				(4, 1, 7, 2)`)

			// Add ratings for player 2 (Bob): skill avg=5, fitness avg=3
			database.DB.Exec(`INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating) VALUES
				(1, 2, 4, 3),
				(3, 2, 6, 3)`)
		})

		It("computes correct averages for players with ratings", func() {
			avgs, err := database.ComputeAverageRatings([]int{1, 2})
			Expect(err).NotTo(HaveOccurred())
			Expect(avgs).To(HaveLen(2))

			// Alice: (6+8+7)/3 = 7, (1+3+2)/3 = 2
			Expect(avgs[1].AvgSkillRating).To(Equal(7.0))
			Expect(avgs[1].AvgFitnessRating).To(Equal(2.0))
			Expect(avgs[1].RatingCount).To(Equal(3))

			// Bob: (4+6)/2 = 5, (3+3)/2 = 3
			Expect(avgs[2].AvgSkillRating).To(Equal(5.0))
			Expect(avgs[2].AvgFitnessRating).To(Equal(3.0))
			Expect(avgs[2].RatingCount).To(Equal(2))
		})

		It("excludes players with no ratings", func() {
			avgs, err := database.ComputeAverageRatings([]int{1, 3}) // Charlie has no ratings
			Expect(err).NotTo(HaveOccurred())
			Expect(avgs).To(HaveLen(1))
			Expect(avgs).To(HaveKey(1))
			Expect(avgs).NotTo(HaveKey(3))
		})

		It("returns empty map for players with no ratings", func() {
			avgs, err := database.ComputeAverageRatings([]int{3, 4})
			Expect(err).NotTo(HaveOccurred())
			Expect(avgs).To(BeEmpty())
		})
	})

	Context("RecalculateAllBaseRatings", func() {
		BeforeEach(func() {
			// Create players with default ratings
			database.DB.Exec(`INSERT INTO players (id, name, phone, base_skill_rating, base_fitness_rating) VALUES
				(1, 'Alice', '+1111', 5.0, 2.0),
				(2, 'Bob', '+2222', 5.0, 2.0),
				(3, 'Charlie', '+3333', 5.0, 2.0),
				(4, 'Diana', '+4444', 5.0, 2.0)`)
		})

		It("updates all players with ratings", func() {
			// Add ratings for Alice and Bob
			database.DB.Exec(`INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating) VALUES
				(2, 1, 8, 3),
				(3, 1, 10, 3),
				(1, 2, 4, 1),
				(3, 2, 6, 1)`)

			count, err := database.RecalculateAllBaseRatings()
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(2)) // Alice and Bob have ratings

			players, _ := database.GetPlayersByIDs([]int{1, 2, 3, 4})
			playerMap := make(map[int]database.Player)
			for _, p := range players {
				playerMap[p.ID] = p
			}

			// Alice: (8+10)/2 = 9, (3+3)/2 = 3
			Expect(playerMap[1].BaseSkillRating).To(Equal(9.0))
			Expect(playerMap[1].BaseFitnessRating).To(Equal(3.0))

			// Bob: (4+6)/2 = 5, (1+1)/2 = 1
			Expect(playerMap[2].BaseSkillRating).To(Equal(5.0))
			Expect(playerMap[2].BaseFitnessRating).To(Equal(1.0))

			// Charlie and Diana unchanged (no ratings)
			Expect(playerMap[3].BaseSkillRating).To(Equal(5.0))
			Expect(playerMap[3].BaseFitnessRating).To(Equal(2.0))
			Expect(playerMap[4].BaseSkillRating).To(Equal(5.0))
			Expect(playerMap[4].BaseFitnessRating).To(Equal(2.0))
		})

		It("returns 0 when no ratings exist", func() {
			count, err := database.RecalculateAllBaseRatings()
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(0))
		})

		It("handles single rating per player", func() {
			database.DB.Exec(`INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating) VALUES
				(2, 1, 7, 2)`)

			count, err := database.RecalculateAllBaseRatings()
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))

			players, _ := database.GetPlayersByIDs([]int{1})
			Expect(players[0].BaseSkillRating).To(Equal(7.0))
			Expect(players[0].BaseFitnessRating).To(Equal(2.0))
		})

		It("processes all players with ratings in one transaction", func() {
			// Add ratings for all 4 players
			database.DB.Exec(`INSERT INTO anonymous_ratings (voter_id, target_id, skill_rating, fitness_rating) VALUES
				(2, 1, 8, 3),
				(1, 2, 6, 2),
				(1, 3, 4, 1),
				(1, 4, 10, 3)`)

			count, err := database.RecalculateAllBaseRatings()
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(4))
		})
	})

	Context("Fitness constants", func() {
		It("defines correct categorical values", func() {
			Expect(database.FitnessLow).To(Equal(1))
			Expect(database.FitnessOk).To(Equal(2))
			Expect(database.FitnessGood).To(Equal(3))
			Expect(database.FitnessGreat).To(Equal(4))
			Expect(database.FitnessExcellent).To(Equal(5))
		})
	})
})
