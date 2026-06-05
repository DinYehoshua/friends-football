package balancer

import (
	"errors"
	"math"
	"math/rand"
)

// Cost weights for the balancing algorithm.
const (
	SkillWeight   = 3.0
	FitnessWeight = 1.0
)

// Early exit thresholds for TotalCost.
const (
	EarlyExitWithFitness    = 7.0 // Exit if TotalCost <= 7.0 when considering fitness
	EarlyExitWithoutFitness = 4.0 // Exit if TotalCost <= 4.0
)

// Player represents a player with their ratings for team balancing.
type Player struct {
	ID            int
	Name          string
	SkillRating   float64 // 1.0 to 10.0
	FitnessRating float64 // 1.0 to 5.0 (VeryPoor=1, Poor=2, Average=3, Good=4, Excellent=5)
}

// Team represents a team of players with aggregate stats.
type Team struct {
	Players      []Player
	TotalSkill   float64
	TotalFitness float64
}

// TeamResult holds the optimal team split with metadata.
type TeamResult struct {
	Home       Team
	Away       Team
	SkillDelta float64 // Absolute difference in skill between teams
	CostDelta  float64 // Absolute difference in fitness between teams (when considerFitness=true)
	TotalCost  float64 // (SkillDelta * 3.0) + (FitnessDelta * 1.0)
}

var (
	ErrInvalidPlayerCount = errors.New("exactly 12 players are required")
)

// GenerateTeams splits 12 players into two balanced teams of 6.
// It shuffles the input slice to ensure different results on repeated runs,
// then evaluates combinations to find the optimal split using cost minimization.
//
// Parameters:
//   - players: exactly 12 players to split
//   - considerFitness: if true, TotalCost = (SkillDelta * 3.0) + (FitnessDelta * 1.0)
//     if false, TotalCost = SkillDelta * 3.0
//
// Returns the single best team combination found (lowest TotalCost).
func GenerateTeams(players []Player, considerFitness bool) (*TeamResult, error) {
	if len(players) != 12 {
		return nil, ErrInvalidPlayerCount
	}

	// Create a copy to avoid mutating the original slice
	shuffled := make([]Player, len(players))
	copy(shuffled, players)

	// Shuffle to ensure different results on repeated runs
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	return findOptimalSplit(shuffled, considerFitness), nil
}

// PlayerScore calculates the score for a player based on the rating criteria.
func PlayerScore(p Player, considerFitness bool) float64 {
	if considerFitness {
		return p.SkillRating + p.FitnessRating
	}
	return p.SkillRating
}

// calculateCost computes the TotalCost for a given split.
func calculateCost(skillDelta, fitnessDelta float64, considerFitness bool) float64 {
	if considerFitness {
		return (skillDelta * SkillWeight) + (fitnessDelta * FitnessWeight)
	}
	return skillDelta * SkillWeight
}

// findOptimalSplit evaluates combinations (C(12,6) = 924) and returns the one with lowest TotalCost.
// Uses dynamic early exit based on considerFitness flag.
func findOptimalSplit(players []Player, considerFitness bool) *TeamResult {
	var bestResult *TeamResult
	bestCost := math.MaxFloat64

	// Determine early exit threshold
	earlyExitThreshold := EarlyExitWithoutFitness
	if considerFitness {
		earlyExitThreshold = EarlyExitWithFitness
	}

	// Generate all combinations of choosing 6 players from 12
	indices := make([]int, 6)
	for i := range indices {
		indices[i] = i
	}

	for {
		// Build teams from current combination
		home := make([]Player, 6)
		awayMap := make(map[int]bool)

		for i, idx := range indices {
			home[i] = players[idx]
			awayMap[idx] = true
		}

		away := make([]Player, 0, 6)
		for i := 0; i < 12; i++ {
			if !awayMap[i] {
				away = append(away, players[i])
			}
		}

		// Calculate team stats in a single pass
		homeSkill, homeFitness := sumRatings(home)
		awaySkill, awayFitness := sumRatings(away)

		skillDelta := math.Abs(homeSkill - awaySkill)
		fitnessDelta := math.Abs(homeFitness - awayFitness)
		totalCost := calculateCost(skillDelta, fitnessDelta, considerFitness)

		// Track the best result (lowest cost)
		if totalCost < bestCost {
			bestCost = totalCost
			bestResult = &TeamResult{
				Home: Team{
					Players:      home,
					TotalSkill:   homeSkill,
					TotalFitness: homeFitness,
				},
				Away: Team{
					Players:      away,
					TotalSkill:   awaySkill,
					TotalFitness: awayFitness,
				},
				SkillDelta: skillDelta,
				CostDelta:  fitnessDelta,
				TotalCost:  totalCost,
			}

			// Dynamic early exit based on considerFitness
			if totalCost <= earlyExitThreshold {
				break
			}
		}

		// Generate next combination
		if !NextCombination(indices, 12) {
			break
		}
	}

	return bestResult
}

// sumRatings calculates total skill and fitness for a team in one pass.
func sumRatings(players []Player) (skill, fitness float64) {
	for _, p := range players {
		skill += p.SkillRating
		fitness += p.FitnessRating
	}
	return
}

// NextCombination generates the next combination in lexicographic order.
// Returns false when all combinations have been exhausted.
func NextCombination(indices []int, n int) bool {
	k := len(indices)

	// Find rightmost element that can be incremented
	i := k - 1
	for i >= 0 && indices[i] == n-k+i {
		i--
	}

	if i < 0 {
		return false
	}

	// Increment and reset subsequent elements
	indices[i]++
	for j := i + 1; j < k; j++ {
		indices[j] = indices[j-1] + 1
	}

	return true
}
