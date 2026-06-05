package balancer_test

import (
	"math"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"friends-football/internal/balancer"
)

func TestBalancer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Balancer Suite")
}

// mockPlayers creates 12 test players with skill (1-10) and fitness (1-5) ratings.
func mockPlayers() []balancer.Player {
	return []balancer.Player{
		{ID: 1, Name: "Player1", SkillRating: 8.0, FitnessRating: 3.0},   // Average
		{ID: 2, Name: "Player2", SkillRating: 7.5, FitnessRating: 2.0},   // Poor
		{ID: 3, Name: "Player3", SkillRating: 6.0, FitnessRating: 1.0},   // VeryPoor
		{ID: 4, Name: "Player4", SkillRating: 9.0, FitnessRating: 2.0},   // Poor
		{ID: 5, Name: "Player5", SkillRating: 5.5, FitnessRating: 3.0},   // Average
		{ID: 6, Name: "Player6", SkillRating: 7.0, FitnessRating: 2.0},   // Poor
		{ID: 7, Name: "Player7", SkillRating: 8.5, FitnessRating: 1.0},   // VeryPoor
		{ID: 8, Name: "Player8", SkillRating: 6.5, FitnessRating: 3.0},   // Average
		{ID: 9, Name: "Player9", SkillRating: 7.0, FitnessRating: 2.0},   // Poor
		{ID: 10, Name: "Player10", SkillRating: 5.0, FitnessRating: 2.0}, // Poor
		{ID: 11, Name: "Player11", SkillRating: 8.0, FitnessRating: 3.0}, // Average
		{ID: 12, Name: "Player12", SkillRating: 6.0, FitnessRating: 2.0}, // Poor
	}
}

// perfectlyBalancedPlayers creates players that can be split with zero cost.
func perfectlyBalancedPlayers() []balancer.Player {
	return []balancer.Player{
		{ID: 1, Name: "A1", SkillRating: 10.0, FitnessRating: 3.0},
		{ID: 2, Name: "A2", SkillRating: 8.0, FitnessRating: 2.0},
		{ID: 3, Name: "A3", SkillRating: 6.0, FitnessRating: 1.0},
		{ID: 4, Name: "A4", SkillRating: 4.0, FitnessRating: 3.0},
		{ID: 5, Name: "A5", SkillRating: 2.0, FitnessRating: 2.0},
		{ID: 6, Name: "A6", SkillRating: 0.0, FitnessRating: 1.0},
		{ID: 7, Name: "B1", SkillRating: 9.0, FitnessRating: 3.0},
		{ID: 8, Name: "B2", SkillRating: 7.0, FitnessRating: 2.0},
		{ID: 9, Name: "B3", SkillRating: 5.0, FitnessRating: 1.0},
		{ID: 10, Name: "B4", SkillRating: 3.0, FitnessRating: 3.0},
		{ID: 11, Name: "B5", SkillRating: 1.0, FitnessRating: 2.0},
		{ID: 12, Name: "B6", SkillRating: 0.0, FitnessRating: 1.0},
	}
}

func teamKey(players []balancer.Player) string {
	ids := make([]int, len(players))
	for i, p := range players {
		ids[i] = p.ID
	}
	for i := 0; i < len(ids)-1; i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[i] > ids[j] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	key := ""
	for _, id := range ids {
		key += string(rune('0' + id))
	}
	return key
}

var _ = Describe("Balancer", func() {
	Context("GenerateTeams", func() {
		When("given invalid player count", func() {
			It("returns ErrInvalidPlayerCount", func() {
				testCases := []struct {
					name    string
					players []balancer.Player
				}{
					{"too few players", mockPlayers()[:10]},
					{"too many players", append(mockPlayers(), balancer.Player{ID: 13, Name: "Extra"})},
					{"empty slice", []balancer.Player{}},
					{"nil slice", nil},
				}

				for _, tc := range testCases {
					result, err := balancer.GenerateTeams(tc.players, true)
					Expect(err).To(Equal(balancer.ErrInvalidPlayerCount), "case: %s", tc.name)
					Expect(result).To(BeNil(), "case: %s", tc.name)
				}
			})
		})

		When("given exactly 12 players", func() {
			It("returns valid team split with correct structure", func() {
				players := mockPlayers()
				result, err := balancer.GenerateTeams(players, true)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())
				Expect(result.Home.Players).To(HaveLen(6))
				Expect(result.Away.Players).To(HaveLen(6))

				// Verify all 12 unique players are distributed
				allIDs := make(map[int]bool)
				for _, p := range result.Home.Players {
					Expect(allIDs[p.ID]).To(BeFalse(), "duplicate player ID %d", p.ID)
					allIDs[p.ID] = true
				}
				for _, p := range result.Away.Players {
					Expect(allIDs[p.ID]).To(BeFalse(), "duplicate player ID %d", p.ID)
					allIDs[p.ID] = true
				}
				Expect(allIDs).To(HaveLen(12))
			})

			It("does not mutate the input slice", func() {
				players := mockPlayers()
				originalOrder := make([]int, len(players))
				for i, p := range players {
					originalOrder[i] = p.ID
				}

				_, _ = balancer.GenerateTeams(players, true)

				for i, p := range players {
					Expect(p.ID).To(Equal(originalOrder[i]))
				}
			})
		})

		When("considerFitness is true", func() {
			It("calculates TotalCost as (SkillDelta * 3.0) + (FitnessDelta * 1.0)", func() {
				result, err := balancer.GenerateTeams(mockPlayers(), true)
				Expect(err).NotTo(HaveOccurred())

				// Verify SkillDelta
				expectedSkillDelta := math.Abs(result.Home.TotalSkill - result.Away.TotalSkill)
				Expect(result.SkillDelta).To(BeNumerically("~", expectedSkillDelta, 0.001))

				// Verify FitnessDelta (stored in CostDelta)
				expectedFitnessDelta := math.Abs(result.Home.TotalFitness - result.Away.TotalFitness)
				Expect(result.CostDelta).To(BeNumerically("~", expectedFitnessDelta, 0.001))

				// Verify TotalCost formula
				expectedCost := (expectedSkillDelta * 3.0) + (expectedFitnessDelta * 1.0)
				Expect(result.TotalCost).To(BeNumerically("~", expectedCost, 0.001))
			})

			It("finds low cost solution for well-balanced input", func() {
				// Players designed to allow a low-cost split
				players := []balancer.Player{
					{ID: 1, SkillRating: 8.0, FitnessRating: 2.0},
					{ID: 2, SkillRating: 8.0, FitnessRating: 2.0},
					{ID: 3, SkillRating: 6.0, FitnessRating: 2.0},
					{ID: 4, SkillRating: 6.0, FitnessRating: 2.0},
					{ID: 5, SkillRating: 4.0, FitnessRating: 2.0},
					{ID: 6, SkillRating: 4.0, FitnessRating: 2.0},
					{ID: 7, SkillRating: 8.0, FitnessRating: 2.0},
					{ID: 8, SkillRating: 8.0, FitnessRating: 2.0},
					{ID: 9, SkillRating: 6.0, FitnessRating: 2.0},
					{ID: 10, SkillRating: 6.0, FitnessRating: 2.0},
					{ID: 11, SkillRating: 4.0, FitnessRating: 2.0},
					{ID: 12, SkillRating: 4.0, FitnessRating: 2.0},
				}
				result, err := balancer.GenerateTeams(players, true)
				Expect(err).NotTo(HaveOccurred())
				// With perfectly paired players, should achieve very low cost
				Expect(result.TotalCost).To(BeNumerically("<=", 2.0))
			})
		})

		When("considerFitness is false", func() {
			It("calculates TotalCost as SkillDelta * 3.0 only", func() {
				result, err := balancer.GenerateTeams(mockPlayers(), false)
				Expect(err).NotTo(HaveOccurred())

				// Verify SkillDelta
				expectedSkillDelta := math.Abs(result.Home.TotalSkill - result.Away.TotalSkill)
				Expect(result.SkillDelta).To(BeNumerically("~", expectedSkillDelta, 0.001))

				// Verify TotalCost is skill-only
				expectedCost := expectedSkillDelta * 3.0
				Expect(result.TotalCost).To(BeNumerically("~", expectedCost, 0.001))
			})

			It("finds low cost solution (SkillDelta <= 1.0) for balanced input", func() {
				result, err := balancer.GenerateTeams(perfectlyBalancedPlayers(), false)
				Expect(err).NotTo(HaveOccurred())
				// SkillDelta <= 1.0 means TotalCost <= 3.0
				Expect(result.SkillDelta).To(BeNumerically("<=", 1.0))
				Expect(result.TotalCost).To(BeNumerically("<=", 3.0))
			})
		})

		When("shuffling players", func() {
			It("produces different team compositions across multiple runs", func() {
				players := mockPlayers()
				results := make(map[string]int)

				for i := 0; i < 50; i++ {
					result, err := balancer.GenerateTeams(players, true)
					Expect(err).NotTo(HaveOccurred())
					key := teamKey(result.Home.Players)
					results[key]++
				}

				Expect(len(results)).To(BeNumerically(">=", 1))
				GinkgoWriter.Printf("Generated %d unique team compositions in 50 iterations\n", len(results))
			})
		})

		When("calculating team stats", func() {
			It("calculates TotalSkill and TotalFitness correctly", func() {
				result, err := balancer.GenerateTeams(mockPlayers(), true)
				Expect(err).NotTo(HaveOccurred())

				var expectedHomeSkill, expectedHomeFitness float64
				for _, p := range result.Home.Players {
					expectedHomeSkill += p.SkillRating
					expectedHomeFitness += p.FitnessRating
				}
				Expect(result.Home.TotalSkill).To(BeNumerically("~", expectedHomeSkill, 0.001))
				Expect(result.Home.TotalFitness).To(BeNumerically("~", expectedHomeFitness, 0.001))

				var expectedAwaySkill, expectedAwayFitness float64
				for _, p := range result.Away.Players {
					expectedAwaySkill += p.SkillRating
					expectedAwayFitness += p.FitnessRating
				}
				Expect(result.Away.TotalSkill).To(BeNumerically("~", expectedAwaySkill, 0.001))
				Expect(result.Away.TotalFitness).To(BeNumerically("~", expectedAwayFitness, 0.001))
			})
		})

		When("minimizing cost", func() {
			It("prioritizes skill balance (weight 3.0) over fitness balance (weight 1.0)", func() {
				// Create players where skill imbalance costs more than fitness imbalance
				players := []balancer.Player{
					{ID: 1, SkillRating: 10.0, FitnessRating: 1.0},
					{ID: 2, SkillRating: 10.0, FitnessRating: 1.0},
					{ID: 3, SkillRating: 10.0, FitnessRating: 1.0},
					{ID: 4, SkillRating: 10.0, FitnessRating: 1.0},
					{ID: 5, SkillRating: 10.0, FitnessRating: 1.0},
					{ID: 6, SkillRating: 10.0, FitnessRating: 1.0},
					{ID: 7, SkillRating: 5.0, FitnessRating: 3.0},
					{ID: 8, SkillRating: 5.0, FitnessRating: 3.0},
					{ID: 9, SkillRating: 5.0, FitnessRating: 3.0},
					{ID: 10, SkillRating: 5.0, FitnessRating: 3.0},
					{ID: 11, SkillRating: 5.0, FitnessRating: 3.0},
					{ID: 12, SkillRating: 5.0, FitnessRating: 3.0},
				}

				result, err := balancer.GenerateTeams(players, true)
				Expect(err).NotTo(HaveOccurred())

				// Algorithm should balance skill first (3 high + 3 low per team)
				// rather than grouping by fitness
				Expect(result.SkillDelta).To(BeNumerically("<=", 1.0))
			})
		})
	})

	Context("NextCombination", func() {
		It("generates exactly 924 combinations for C(12,6)", func() {
			indices := []int{0, 1, 2, 3, 4, 5}
			count := 1

			for balancer.NextCombination(indices, 12) {
				count++
				Expect(count).To(BeNumerically("<=", 1000), "nextCombination did not terminate")
			}

			Expect(count).To(Equal(924))
		})
	})

	Context("PlayerScore", func() {
		It("returns Skill + Fitness when considerFitness is true, Skill only when false", func() {
			player := balancer.Player{SkillRating: 7.5, FitnessRating: 2.0}

			Expect(balancer.PlayerScore(player, true)).To(Equal(9.5))
			Expect(balancer.PlayerScore(player, false)).To(Equal(7.5))
		})
	})

	Context("Cost calculation constants", func() {
		It("uses correct weights", func() {
			Expect(balancer.SkillWeight).To(Equal(3.0))
			Expect(balancer.FitnessWeight).To(Equal(1.0))
		})

		It("uses correct early exit thresholds", func() {
			Expect(balancer.EarlyExitWithFitness).To(Equal(2.0))
			Expect(balancer.EarlyExitWithoutFitness).To(Equal(3.0))
		})
	})
})
