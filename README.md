# Friends Football Balancer

A web application for amateur soccer organizers to automate weekly team balancing. Parse player registrations from WhatsApp chat exports, collect anonymous peer ratings, and generate fair teams using a cost-minimization algorithm.

## Features

- **WhatsApp Integration**: Upload a WhatsApp chat export (.zip) and automatically extract player registrations using Google Gemini AI
- **Anonymous Peer Ratings**: Players rate each other's skill (1-10) and fitness (1-3) without seeing others' ratings
- **Smart Team Balancing**: Evaluates all 924 possible team combinations to find the optimal split
- **Alias Learning**: Unrecognized names can be mapped to existing players, permanently learning nicknames
- **Guest Support**: Handle one-time guests with customizable skill/fitness ratings
- **Mobile-First UI**: Dark theme inspired by Apple Sports App, fully responsive

## Quick Start

### Prerequisites

- Go 1.21+
- Google Gemini API key (for WhatsApp parsing)

### Run Locally

```bash
# Clone and enter directory
git clone <repo-url>
cd friends-football

# Set required environment variable
export GEMINI_API_KEY=your-api-key

# Run (uses SQLite by default)
go run main.go

# Open http://localhost:8080
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `:8080` | HTTP server port |
| `DB_PATH` | `./friends-football.db` | SQLite database path |
| `DATABASE_URL` | - | PostgreSQL connection string (overrides SQLite) |
| `SEED_FILE` | `players_seed.json` | Initial player data file |
| `GEMINI_API_KEY` | - | **Required** for WhatsApp chat parsing |

### Seed Data

Create `players_seed.json` to bootstrap your player database:

```json
[
  {
    "name": "Omer",
    "phone": "+972501234567",
    "nickname_aliases": ["Omeri", "עומר"],
    "is_admin": true,
    "tier": 1
  },
  {
    "name": "Dan",
    "phone": "+972502345678",
    "nickname_aliases": ["Danny"],
    "is_admin": false,
    "tier": 2
  }
]
```

Tiers: 1=Core, 2=Regular, 3=Occasional, 4=Rare

## Usage

### For Players (Rate Your Friends)

1. Login with your phone number
2. Rate other players' skill (1-10 slider) and fitness (Poor/Normal/Good)
3. Ratings are anonymous and auto-saved

### For Admins (Your Kohot)

1. **Upload**: Drag-and-drop WhatsApp chat export (.zip)
2. **Resolve**: Map any unrecognized names to existing players or mark as guests
3. **Review**: Verify the 12 players, adjust guest ratings if needed
4. **Generate**: Click to balance teams with optional fitness consideration
5. **Share**: Copy formatted team list to clipboard for WhatsApp

## Project Structure

```
main.go                     # Entry point, DB init, graceful shutdown
frontend/
  embed.go                  # go:embed directive for static files
  static/index.html         # Complete SPA (HTML + CSS + JS)
internal/
  database/database.go      # Dual SQLite/PostgreSQL support
  balancer/balancer.go      # 924-permutation team optimizer
  parser/parser.go          # WhatsApp parsing via Gemini API
  server/
    server.go               # HTTP router, middleware
    auth_handlers.go        # Login, sessions, ratings
    admin_handlers.go       # Upload, team generation
players_seed.json           # Optional seed data
```

## API Endpoints

### Authentication

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/auth/login` | Login with phone number |
| GET | `/api/players` | List all players with your ratings |
| POST | `/api/ratings` | Submit batch ratings |

### Admin (requires admin role)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/admin/upload` | Upload WhatsApp ZIP |
| POST | `/api/admin/resolve-aliases` | Map unrecognized names |
| POST | `/api/admin/generate-teams` | Generate balanced teams |
| POST | `/api/admin/recalculate-base-ratings` | Sync base ratings from peer averages |

## Algorithm

The team balancer minimizes a weighted cost function:

```
TotalCost = (SkillDelta × 3.0) + (FitnessDelta × 1.0)
```

- Evaluates all C(12,6) = 924 combinations
- Early exit when cost ≤ 7.0 (with fitness) or ≤ 4.0 (skill only)
- Shuffles players before evaluation for varied results on reruns

## Tech Stack

- **Backend**: Go standard library (`net/http`)
- **Database**: SQLite (dev) / PostgreSQL (prod)
- **Frontend**: Vanilla JS + Tailwind CSS (via CDN)
- **AI**: Google Gemini API for chat parsing
- **Deployment**: Single binary with embedded frontend

## Development

```bash
# Run tests
go test ./...

# Run with verbose logging
go run main.go

# Build production binary
go build -o friends-football main.go
```

## License

Private project - all rights reserved.
