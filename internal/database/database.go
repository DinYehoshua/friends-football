package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	_ "github.com/lib/pq"           // PostgreSQL driver
	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// DriverType represents the active database driver.
type DriverType string

const (
	DriverSQLite   DriverType = "sqlite3"
	DriverPostgres DriverType = "postgres"
)

// FitnessCategory represents the categorical fitness rating (1-5 scale).
const (
	FitnessLow       = 1 // Low - Struggles with continuous running
	FitnessPoor      = 2 // Poor - Tires quickly
	FitnessAverage   = 3 // Average - Standard match pace
	FitnessGood      = 4 // Good - High work rate
	FitnessExcellent = 5 // Excellent - Tireless, peak stamina
)

// DB holds the database connection pool.
var DB *sql.DB

// ActiveDriver tracks which driver is currently in use.
var ActiveDriver DriverType

// sqliteSchema contains the DDL statements for SQLite.
const sqliteSchema = `
-- Main Players Table
CREATE TABLE IF NOT EXISTS players (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    phone TEXT UNIQUE NOT NULL,
    nickname_aliases TEXT,
    base_skill_rating REAL DEFAULT 5.0,
    base_fitness_rating REAL DEFAULT 3.0,
    is_admin INTEGER DEFAULT 0,
    tier INTEGER DEFAULT 3
);

-- Anonymous Peer Ratings Table
CREATE TABLE IF NOT EXISTS anonymous_ratings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    voter_id INTEGER REFERENCES players(id),
    target_id INTEGER REFERENCES players(id),
    skill_rating INTEGER CHECK (skill_rating BETWEEN 1 AND 10),
    fitness_rating INTEGER CHECK (fitness_rating BETWEEN 1 AND 5),
    UNIQUE(voter_id, target_id)
);

-- Index for faster lookups by phone number
CREATE INDEX IF NOT EXISTS idx_players_phone ON players(phone);

-- Index for faster rating lookups by target
CREATE INDEX IF NOT EXISTS idx_ratings_target ON anonymous_ratings(target_id);
`

// postgresSchema contains the DDL statements for PostgreSQL.
const postgresSchema = `
-- Main Players Table
CREATE TABLE IF NOT EXISTS players (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    phone TEXT UNIQUE NOT NULL,
    nickname_aliases TEXT,
    base_skill_rating DOUBLE PRECISION DEFAULT 5.0,
    base_fitness_rating DOUBLE PRECISION DEFAULT 3.0,
    is_admin BOOLEAN DEFAULT FALSE,
    tier INTEGER DEFAULT 3
);

-- Anonymous Peer Ratings Table
CREATE TABLE IF NOT EXISTS anonymous_ratings (
    id SERIAL PRIMARY KEY,
    voter_id INTEGER REFERENCES players(id),
    target_id INTEGER REFERENCES players(id),
    skill_rating INTEGER CHECK (skill_rating BETWEEN 1 AND 10),
    fitness_rating INTEGER CHECK (fitness_rating BETWEEN 1 AND 5),
    UNIQUE(voter_id, target_id)
);

-- Index for faster lookups by phone number
CREATE INDEX IF NOT EXISTS idx_players_phone ON players(phone);

-- Index for faster rating lookups by target
CREATE INDEX IF NOT EXISTS idx_ratings_target ON anonymous_ratings(target_id);
`

// Player represents a player record from the database.
type Player struct {
	ID                int
	Name              string
	Phone             string
	NicknameAliases   sql.NullString
	BaseSkillRating   float64
	BaseFitnessRating float64
	IsAdmin           bool
	Tier              int // 1=Core, 2=Regular, 3=Occasional, 4=Rare
}

// Init opens a database connection with automatic driver selection.
// If DATABASE_URL env var is set and starts with postgres://, uses PostgreSQL.
// Otherwise, falls back to SQLite using the provided dbPath.
func Init(dbPath string) error {
	// Check for PostgreSQL connection string
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL != "" && (strings.HasPrefix(databaseURL, "postgres://") || strings.HasPrefix(databaseURL, "postgresql://")) {
		return initPostgres(databaseURL)
	}

	// Fall back to SQLite
	return initSQLite(dbPath)
}

// initPostgres initializes a PostgreSQL connection.
func initPostgres(databaseURL string) error {
	var err error
	ActiveDriver = DriverPostgres

	DB, err = sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("failed to open PostgreSQL database: %w", err)
	}

	// Verify the connection is valid
	if err = DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping PostgreSQL database: %w", err)
	}

	// Run migrations
	if err = migrate(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Seed data if empty
	if err = seedIfEmpty(); err != nil {
		return fmt.Errorf("failed to seed database: %w", err)
	}

	log.Println("PostgreSQL database initialized successfully")
	return nil
}

// initSQLite initializes a SQLite connection.
func initSQLite(dbPath string) error {
	var err error
	ActiveDriver = DriverSQLite

	DB, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// Verify the connection is valid
	if err = DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping SQLite database: %w", err)
	}

	// Enable foreign key support (disabled by default in SQLite)
	if _, err = DB.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Run migrations
	if err = migrate(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Seed data if empty
	if err = seedIfEmpty(); err != nil {
		return fmt.Errorf("failed to seed database: %w", err)
	}

	log.Println("SQLite database initialized successfully")
	return nil
}

// migrate executes the DDL statements based on the active driver.
func migrate() error {
	var schema string
	switch ActiveDriver {
	case DriverPostgres:
		schema = postgresSchema
	default:
		schema = sqliteSchema
	}

	_, err := DB.Exec(schema)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	log.Printf("Database migrations completed (%s)", ActiveDriver)
	return nil
}

// Close safely closes the database connection.
func Close() error {
	if DB != nil {
		log.Println("Closing database connection")
		return DB.Close()
	}
	return nil
}

// Placeholder returns the correct placeholder syntax for the active driver.
// For SQLite: returns "?"
// For PostgreSQL: returns "$N" where N is the position (1-indexed)
func Placeholder(position int) string {
	if ActiveDriver == DriverPostgres {
		return "$" + strconv.Itoa(position)
	}
	return "?"
}

// BuildPlaceholders returns a comma-separated list of placeholders for N values.
// For SQLite: "?, ?, ?" (for N=3)
// For PostgreSQL: "$1, $2, $3" (for N=3, starting at offset)
func BuildPlaceholders(count, offset int) string {
	placeholders := make([]string, count)
	for i := 0; i < count; i++ {
		placeholders[i] = Placeholder(offset + i + 1)
	}
	return strings.Join(placeholders, ", ")
}

// BuildInClause returns an IN clause with proper placeholders.
// Returns the clause string and updates the args slice.
func BuildInClause(ids []int) (string, []interface{}) {
	args := make([]interface{}, len(ids))
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id
		placeholders[i] = Placeholder(i + 1)
	}
	return strings.Join(placeholders, ", "), args
}

// GetPlayersByIDs retrieves players by their IDs.
func GetPlayersByIDs(ids []int) ([]Player, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders, args := BuildInClause(ids)
	query := fmt.Sprintf(`SELECT id, name, phone, nickname_aliases, base_skill_rating, base_fitness_rating, is_admin, tier
	          FROM players WHERE id IN (%s)`, placeholders)

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query players: %w", err)
	}
	defer rows.Close()

	var players []Player
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.ID, &p.Name, &p.Phone, &p.NicknameAliases, &p.BaseSkillRating, &p.BaseFitnessRating, &p.IsAdmin, &p.Tier); err != nil {
			return nil, fmt.Errorf("failed to scan player: %w", err)
		}
		players = append(players, p)
	}

	return players, rows.Err()
}

// PlayerRatingAvg holds the computed average ratings for a player.
type PlayerRatingAvg struct {
	PlayerID         int
	AvgSkillRating   float64
	AvgFitnessRating float64
	RatingCount      int
}

// ComputeAverageRatings calculates the average anonymous peer ratings for the given player IDs.
// Returns a map of playerID -> PlayerRatingAvg.
func ComputeAverageRatings(playerIDs []int) (map[int]PlayerRatingAvg, error) {
	if len(playerIDs) == 0 {
		return nil, nil
	}

	placeholders, args := BuildInClause(playerIDs)
	query := fmt.Sprintf(`SELECT target_id, AVG(skill_rating), AVG(fitness_rating), COUNT(*)
	          FROM anonymous_ratings WHERE target_id IN (%s) GROUP BY target_id`, placeholders)

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query average ratings: %w", err)
	}
	defer rows.Close()

	result := make(map[int]PlayerRatingAvg)
	for rows.Next() {
		var avg PlayerRatingAvg
		if err := rows.Scan(&avg.PlayerID, &avg.AvgSkillRating, &avg.AvgFitnessRating, &avg.RatingCount); err != nil {
			return nil, fmt.Errorf("failed to scan average rating: %w", err)
		}
		result[avg.PlayerID] = avg
	}

	return result, rows.Err()
}

// RecalculateAllBaseRatings computes the average anonymous ratings for ALL players
// who have received at least one rating and updates their base_skill_rating and
// base_fitness_rating columns. Returns the count of players updated.
func RecalculateAllBaseRatings() (int, error) {
	// Get all averages grouped by target_id
	query := `SELECT target_id, AVG(skill_rating), AVG(fitness_rating)
	          FROM anonymous_ratings GROUP BY target_id`

	rows, err := DB.Query(query)
	if err != nil {
		return 0, fmt.Errorf("failed to query average ratings: %w", err)
	}
	defer rows.Close()

	type avgRating struct {
		targetID   int
		avgSkill   float64
		avgFitness float64
	}
	var averages []avgRating

	for rows.Next() {
		var avg avgRating
		if err := rows.Scan(&avg.targetID, &avg.avgSkill, &avg.avgFitness); err != nil {
			return 0, fmt.Errorf("failed to scan average rating: %w", err)
		}
		averages = append(averages, avg)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("error iterating ratings: %w", err)
	}

	if len(averages) == 0 {
		return 0, nil // No ratings to sync
	}

	// Begin transaction for batch update
	tx, err := DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Build update query with proper placeholders
	updateQuery := fmt.Sprintf(`UPDATE players SET base_skill_rating = %s, base_fitness_rating = %s WHERE id = %s`,
		Placeholder(1), Placeholder(2), Placeholder(3))

	stmt, err := tx.Prepare(updateQuery)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare update statement: %w", err)
	}
	defer stmt.Close()

	for _, avg := range averages {
		if _, err := stmt.Exec(avg.avgSkill, avg.avgFitness, avg.targetID); err != nil {
			return 0, fmt.Errorf("failed to update player %d: %w", avg.targetID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Recalculated base ratings for %d players", len(averages))
	return len(averages), nil
}

// SeedPlayer represents a player in the seed JSON file.
// Note: base_skill_rating and base_fitness_rating are omitted;
// they use schema defaults (5.0 and 2.0 respectively).
type SeedPlayer struct {
	Name            string   `json:"name"`
	Phone           string   `json:"phone"`
	NicknameAliases []string `json:"nickname_aliases,omitempty"`
	IsAdmin         bool     `json:"is_admin"`
	Tier            int      `json:"tier"`
}

// seedIfEmpty checks if the players table is empty and seeds it from players_seed.json if found.
func seedIfEmpty() error {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM players").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count players: %w", err)
	}

	if count > 0 {
		log.Printf("Database already has %d players, skipping seed", count)
		return nil
	}

	// Try to load seed file
	seedPath := os.Getenv("SEED_FILE")
	if seedPath == "" {
		seedPath = "players_seed.json"
	}

	data, err := os.ReadFile(seedPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Println("No seed file found, skipping database seeding")
			return nil
		}
		return fmt.Errorf("failed to read seed file: %w", err)
	}

	var players []SeedPlayer
	if err := json.Unmarshal(data, &players); err != nil {
		return fmt.Errorf("failed to parse seed file: %w", err)
	}

	if len(players) == 0 {
		log.Println("Seed file is empty, skipping database seeding")
		return nil
	}

	// Begin transaction for bulk insert
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin seed transaction: %w", err)
	}
	defer tx.Rollback()

	// Build insert query with proper placeholders
	// Note: base_skill_rating and base_fitness_rating use schema defaults (5.0 and 2.0)
	insertQuery := fmt.Sprintf(`INSERT INTO players (name, phone, nickname_aliases, is_admin, tier)
		VALUES (%s, %s, %s, %s, %s)`,
		Placeholder(1), Placeholder(2), Placeholder(3), Placeholder(4), Placeholder(5))

	stmt, err := tx.Prepare(insertQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare seed insert: %w", err)
	}
	defer stmt.Close()

	for _, p := range players {
		var aliasesJSON sql.NullString
		if len(p.NicknameAliases) > 0 {
			aliasBytes, _ := json.Marshal(p.NicknameAliases)
			aliasesJSON = sql.NullString{String: string(aliasBytes), Valid: true}
		}

		// Handle is_admin based on driver
		var isAdmin interface{}
		if ActiveDriver == DriverPostgres {
			isAdmin = p.IsAdmin // PostgreSQL accepts bool directly
		} else {
			if p.IsAdmin {
				isAdmin = 1
			} else {
				isAdmin = 0
			}
		}

		if _, err := stmt.Exec(p.Name, p.Phone, aliasesJSON, isAdmin, p.Tier); err != nil {
			return fmt.Errorf("failed to insert player %s: %w", p.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit seed transaction: %w", err)
	}

	log.Printf("Seeded database with %d players from %s", len(players), seedPath)
	return nil
}
