# Project Specification: Friends Football Balancer

## Overview
Friends Football Balancer is a production-grade web application designed for amateur soccer organizers to automate the weekly process of parsing player registrations from a WhatsApp chat export, collecting anonymous peer ratings, and generating balanced teams via a cost-minimization algorithm.

## Tech Stack & Project Structure
- **Backend:** Go (Golang) standard library (`net/http` router via `http.ServeMux`)
- **Embedding:** Native Go `go:embed` compiling all static frontend assets into a single executable binary
- **Database:** Dual-driver support - SQLite (local dev) or PostgreSQL (cloud deployment via `DATABASE_URL`)
- **Frontend:** Single Page Application (SPA) using HTML5, Vanilla JavaScript (Fetch API), and Tailwind CSS via CDN
- **LLM Integration:** Google Gemini API (`gemini-flash-latest`) for WhatsApp chat parsing
- **UI/UX Style:** Dark Mode inspired by Apple Sports App (pitch black `#000000`, dark grey borders `#2C2C2E`, white typography, green accents)

### Project Directory Tree
```
main.go                              -> Application entry point, DB init, graceful shutdown
frontend/
  embed.go                           -> go:embed directive for static files
  static/index.html                  -> Complete SPA with all views
internal/
  database/database.go               -> Dual SQLite/PostgreSQL schemas, migrations, rating queries
  balancer/balancer.go               -> 924-permutation combinatorial optimizer
  parser/parser.go                   -> WhatsApp zip parsing via Gemini API
  server/
    server.go                        -> HTTP router, middleware, static file serving
    auth_handlers.go                 -> Login, session tokens, player/rating endpoints
    admin_handlers.go                -> Upload, team generation, rating recalculation
players_seed.json                    -> Optional seed data for empty databases
```

---

## Architecture & Data Flow

### 1. Database Schema (SQLite / PostgreSQL)

The schema uses driver-specific syntax with automatic detection based on `DATABASE_URL` environment variable.

**SQLite Schema:**
```sql
CREATE TABLE IF NOT EXISTS players (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    phone TEXT UNIQUE NOT NULL,
    nickname_aliases TEXT,              -- JSON array: ["Din", "דין"]
    base_skill_rating REAL DEFAULT 5.0, -- 1.0 to 10.0
    base_fitness_rating REAL DEFAULT 2.0, -- 1.0 to 3.0
    is_admin INTEGER DEFAULT 0,         -- 0 = Player, 1 = Admin
    tier INTEGER DEFAULT 3              -- 1=Core, 2=Regular, 3=Occasional, 4=Rare
);

CREATE TABLE IF NOT EXISTS anonymous_ratings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    voter_id INTEGER REFERENCES players(id),
    target_id INTEGER REFERENCES players(id),
    skill_rating INTEGER CHECK (skill_rating BETWEEN 1 AND 10),
    fitness_rating INTEGER CHECK (fitness_rating BETWEEN 1 AND 3),
    UNIQUE(voter_id, target_id)
);
```

**PostgreSQL Schema:**
```sql
CREATE TABLE IF NOT EXISTS players (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    phone TEXT UNIQUE NOT NULL,
    nickname_aliases TEXT,
    base_skill_rating DOUBLE PRECISION DEFAULT 5.0,
    base_fitness_rating DOUBLE PRECISION DEFAULT 2.0,
    is_admin BOOLEAN DEFAULT FALSE,
    tier INTEGER DEFAULT 3
);

CREATE TABLE IF NOT EXISTS anonymous_ratings (
    id SERIAL PRIMARY KEY,
    voter_id INTEGER REFERENCES players(id),
    target_id INTEGER REFERENCES players(id),
    skill_rating INTEGER CHECK (skill_rating BETWEEN 1 AND 10),
    fitness_rating INTEGER CHECK (fitness_rating BETWEEN 1 AND 3),
    UNIQUE(voter_id, target_id)
);
```

**Database Seeding:** On startup, if the `players` table is empty and `players_seed.json` exists (or `SEED_FILE` env var points to one), the database is auto-seeded. Seed files contain only metadata (`name`, `phone`, `nickname_aliases`, `is_admin`, `tier`); ratings use schema defaults.

### 2. Team Balancing Algorithm (Cost Minimization)

- **Input:** Exactly 12 players with skill (1-10) and fitness (1-3) ratings
- **Shuffling:** Deep-copies and shuffles players to ensure varied outputs on repeated runs
- **Permutations:** Evaluates all 924 combinations (C(12,6)) to find optimal split
- **Cost Formula:**
  ```
  TotalCost = (SkillDelta × 3.0) + (FitnessDelta × 1.0)
  ```
- **Early Exit Thresholds:** Exits early when a "good enough" solution is found:
  - With fitness: `TotalCost ≤ 7.0`
  - Without fitness: `TotalCost ≤ 4.0`
- **Rating Source:** Uses peer-averaged ratings from `anonymous_ratings` when available, falling back to `base_*_rating` columns

### 3. WhatsApp Chat Parsing (Gemini LLM)

- **Model:** `gemini-flash-latest` with structured JSON output
- **Input:** ZIP file containing `_chat.txt` (WhatsApp export format)
- **Logic:** Parses running count registration messages chronologically (e.g., "Omer: 1", "Dan: 2", "Cancel 11")
- **Resolution Pipeline:**
  1. Extract 12 player name strings via LLM
  2. Match against database: exact name → phone → nickname alias → case-insensitive substring
  3. Unresolved names become guest players (ID=-1, default ratings 5.0/2.0)

---

## API Contract

### Authentication & Player Portal

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/auth/login` | POST | Login with phone number, returns session cookie |
| `/api/players` | GET | Get all players (except self) with voter's existing ratings |
| `/api/ratings` | POST | Batch upsert anonymous ratings |

**Login Request/Response:**
```json
// POST /api/auth/login
// Request:
{"phone": "0501234567"}

// Response:
{"player_id": 1, "name": "Omer", "is_admin": true}
// + Set-Cookie: ff_session=<signed-token>
```

**Players Response:**
```json
// GET /api/players
[
  {
    "id": 2,
    "name": "Dan",
    "tier": 1,
    "base_skill_rating": 7.5,
    "base_fitness_rating": 2.0,
    "my_skill_rating": 8,        // null if not rated
    "my_fitness_rating": "Good"  // "Low" | "Ok" | "Good" | "Great" | "Excellent" | null
  }
]
```

**Ratings Request:**
```json
// POST /api/ratings
[
  {"target_id": 2, "skill_rating": 8, "fitness_category": "Good"},
  {"target_id": 3, "skill_rating": 6, "fitness_category": "Normal"}
]

// Response:
{"success": true, "saved_count": 2}
```

### Admin Console (Protected by `requireAdmin` Middleware)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/admin/upload` | POST | Upload WhatsApp ZIP, parse and resolve players |
| `/api/admin/resolve-aliases` | POST | Map unrecognized names to players or mark as guests |
| `/api/admin/generate-teams` | POST | Generate balanced teams from 12 players |
| `/api/admin/recalculate-base-ratings` | POST | Sync all base ratings from peer averages |

**Upload Response:**
```json
// POST /api/admin/upload (multipart/form-data, field: chat_file)
{
  "players": [{"id": 1, "name": "Omer"}, {"id": -1, "name": "Guest Player"}],
  "unresolved": [{"name": "Guest Player", "index": 11}],
  "registered_players": [{"id": 1, "name": "Omer"}, {"id": 2, "name": "Dan"}]  // Only when unresolved is not empty
}
```

**Resolve Aliases Request/Response:**
```json
// POST /api/admin/resolve-aliases
// Request:
{
  "mappings": [
    {"unresolved_name": "יוסי", "player_id": 5, "index": 2},
    {"unresolved_name": "Guest Player", "player_id": -1, "index": 11}
  ]
}

// Response:
{
  "success": true,
  "aliases_saved": 1,
  "players": [{"id": 5, "name": "Yossi"}, {"id": -1, "name": "Guest Player"}]
}
```

**Generate Teams Request/Response:**
```json
// POST /api/admin/generate-teams
// Request:
{
  "players": [{"id": 1}, {"id": 2}, {"name": "Guest"}, ...],
  "consider_fitness": true
}

// Response:
{
  "home": {
    "players": [...],
    "total_skill": 42.5,
    "total_fitness": 14.0
  },
  "away": {
    "players": [...],
    "total_skill": 42.0,
    "total_fitness": 14.0
  },
  "skill_delta": 0.5,
  "fitness_delta": 0.0,
  "total_cost": 1.5
}
```

**Recalculate Ratings Response:**
```json
// POST /api/admin/recalculate-base-ratings
{"success": true, "players_updated": 15}
```

---

## Frontend UI/UX (Apple Sports Theme)

### Global Navigation
- Top navigation with "Rate Your Friends" and "Your Kohot" tabs
- Admin-only tabs are visible but disabled for regular players (greyed out with lock indicator)
- Session-based greeting with randomized messages

### "Rate Your Friends" View
- Displays all players sorted by tier (Core → Regular → Occasional → Rare), then name
- Each player card shows:
  - Name and tier badge
  - Skill slider (1-10) with numeric display
  - Fitness dropdown ("Low" | "Ok" | "Good" | "Great" | "Excellent")
- Ratings auto-save on change via batch endpoint
- Mobile-optimized with responsive controls and touch-friendly sizing

### "Your Kohot" View (Admin Dashboard)
1. **Upload Zone:** Drag-and-drop area for WhatsApp ZIP files
2. **Player List:** 12 editable input fields for extracted/resolved names
3. **Fitness Toggle:** Enable/disable fitness consideration in balancing
4. **Generate Button:** Triggers team generation
5. **Results Display:**
   - Side-by-side columns: "⚪ White Team" and "🔵 Blue Team"
   - Player names only (ratings hidden for anonymity)
   - Drag-and-drop reordering within/between teams
   - Mobile: Touch drag via handle icons (⋮⋮)
6. **Share Button:** Copies formatted roster to clipboard for WhatsApp

### Mobile Responsiveness (≤768px)
- Condensed rating controls
- Touch-friendly drag handles
- Full-width cards with proper overflow handling
- Responsive grid layouts

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `:8080` | HTTP server port |
| `DB_PATH` | `./friends-football.db` | SQLite database file path |
| `DATABASE_URL` | - | PostgreSQL connection string (overrides SQLite) |
| `SEED_FILE` | `players_seed.json` | Path to seed data file |
| `GEMINI_API_KEY` | - | Required for WhatsApp chat parsing |

---

## Session Management

- **Cookie:** `ff_session` (HttpOnly, SameSite=Lax, 7-day expiry)
- **Token Format:** Base64-encoded `{playerID}:{timestamp}.{hmac-signature}`
- **Secret:** Randomly generated on server startup (32 bytes)
