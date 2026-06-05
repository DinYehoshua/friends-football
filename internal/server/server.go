// Package server provides the HTTP API for Friends Football.
//
// # API Endpoints
//
// ## Authentication & Player Portal
//
//	POST /api/auth/login
//	  - Login with phone number, sets session cookie
//	  - Body: {"phone": "+972501234567"}
//	  - Response: {"player_id": 1, "name": "Omer", "is_admin": false}
//
//	GET /api/players
//	  - Get all players except the logged-in user (for rating)
//	  - Requires: session cookie
//	  - Response: [{"id": 1, "name": "Dan", ...}, ...]
//
//	POST /api/ratings
//	  - Submit batch of anonymous ratings for players
//	  - Requires: session cookie
//	  - Body: [{"target_id": 2, "skill_rating": 7, "fitness_category": "Good"}, ...]
//	  - fitness_category: "Very Poor" (1), "Poor" (2), "Average" (3), "Good" (4), "Excellent" (5)
//	  - Response: {"success": true, "saved_count": 5}
//
// ## Admin Dashboard (requires is_admin=1)
//
//	POST /api/admin/upload
//	  - Upload WhatsApp chat zip file, parse and extract 12 players
//	  - Requires: session cookie (admin only)
//	  - Content-Type: multipart/form-data
//	  - Form field: "chat_file" (the .zip file)
//	  - Response: {"players": [...], "unresolved": [...]}
//
//	POST /api/admin/generate-teams
//	  - Generate balanced teams from 12 players (by ID or name)
//	  - Requires: session cookie (admin only)
//	  - Body: {"players": [{"id": 1}, {"name": "Guest"}, ...], "consider_fitness": true}
//	  - Response: {"home": {...}, "away": {...}, "skill_delta": 0.5, "total_cost": 1.5}
//
//	POST /api/admin/recalculate-base-ratings
//	  - Recalculate all players' base ratings from anonymous peer ratings
//	  - Requires: session cookie (admin only)
//	  - Response: {"success": true, "players_updated": 15}
//
// # cURL Examples
//
//	# Login
//	curl -X POST http://localhost:8080/api/auth/login \
//	  -H "Content-Type: application/json" \
//	  -d '{"phone": "+972501234567"}' \
//	  -c cookies.txt
//
//	# Get players for rating
//	curl http://localhost:8080/api/players -b cookies.txt
//
//	# Submit batch ratings
//	curl -X POST http://localhost:8080/api/ratings \
//	  -H "Content-Type: application/json" \
//	  -d '[{"target_id": 2, "skill_rating": 8, "fitness_category": "Good"}]' \
//	  -b cookies.txt
//
//	# Upload chat file (admin only)
//	curl -X POST http://localhost:8080/api/admin/upload \
//	  -F "chat_file=@WhatsApp Chat.zip" \
//	  -b cookies.txt
//
//	# Generate teams (admin only)
//	curl -X POST http://localhost:8080/api/admin/generate-teams \
//	  -H "Content-Type: application/json" \
//	  -d '{"players": [{"id":1},{"id":2},{"name":"Guest"},...], "consider_fitness": true}' \
//	  -b cookies.txt
package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"time"

	"friends-football/internal/database"
)

const (
	defaultPort       = ":8080"
	sessionCookieName = "ff_session"
	cookieMaxAge      = 86400 * 7 // 7 days
)

// Server represents the HTTP server for Friends Football.
type Server struct {
	httpServer  *http.Server
	mux         *http.ServeMux
	staticFiles embed.FS
}

// Config holds server configuration.
type Config struct {
	Port        string
	StaticFiles embed.FS // Embedded static files from frontend package
}

// New creates a new Server with configured routes.
func New(cfg Config) *Server {
	if cfg.Port == "" {
		cfg.Port = defaultPort
	}

	mux := http.NewServeMux()
	s := &Server{
		mux:         mux,
		staticFiles: cfg.StaticFiles,
		httpServer: &http.Server{
			Addr:         cfg.Port,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 45 * time.Second, // Longer timeout for Gemini API calls
			IdleTimeout:  60 * time.Second,
		},
	}

	s.registerRoutes()
	return s
}

// registerRoutes sets up all API routes.
func (s *Server) registerRoutes() {
	// Auth & Player endpoints
	s.mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	s.mux.HandleFunc("GET /api/players", s.handleGetPlayers)
	s.mux.HandleFunc("POST /api/ratings", s.handleSubmitRatings)

	// Admin endpoints (require admin role)
	s.mux.HandleFunc("POST /api/admin/upload", s.requireAdmin(s.handleUploadChat))
	s.mux.HandleFunc("POST /api/admin/generate-teams", s.requireAdmin(s.handleGenerateTeams))
	s.mux.HandleFunc("POST /api/admin/recalculate-base-ratings", s.requireAdmin(s.handleRecalculateBaseRatings))
	s.mux.HandleFunc("POST /api/admin/resolve-aliases", s.requireAdmin(s.handleResolveAliases))

	// Health check
	s.mux.HandleFunc("GET /health", s.handleHealth)

	// Static files (frontend SPA)
	s.registerStaticFiles()
}

// registerStaticFiles serves embedded static files at root.
func (s *Server) registerStaticFiles() {
	// Get the "static" subdirectory from the embedded FS
	staticFS, err := fs.Sub(s.staticFiles, "static")
	if err != nil {
		log.Printf("Warning: static files not available: %v", err)
		return
	}

	// Serve static files, with index.html as fallback for SPA routing
	fileServer := http.FileServer(http.FS(staticFS))
	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the requested file
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if file exists
		if _, err := fs.Stat(staticFS, path[1:]); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fallback to index.html for SPA routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

// requireAdmin wraps a handler to enforce admin-only access.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		playerID, err := getSessionPlayerID(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "Not authenticated")
			return
		}

		// Check if player is admin
		var isAdmin bool
		query := fmt.Sprintf(`SELECT is_admin FROM players WHERE id = %s`, database.Placeholder(1))
		err = database.DB.QueryRow(query, playerID).Scan(&isAdmin)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "Player not found")
			return
		}

		if !isAdmin {
			writeError(w, http.StatusForbidden, "Only the manager can do that!")
			return
		}

		next(w, r)
	}
}

// Start begins listening for HTTP requests.
func (s *Server) Start() error {
	log.Printf("Starting HTTP server on %s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Shutting down HTTP server...")
	return s.httpServer.Shutdown(ctx)
}

// ServeHTTP implements http.Handler for testing.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// handleHealth returns server health status.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeJSON sends a JSON response.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

// writeError sends a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// decodeJSON decodes JSON request body into v.
func decodeJSON(r *http.Request, v interface{}) error {
	if r.Body == nil {
		return fmt.Errorf("empty request body")
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
