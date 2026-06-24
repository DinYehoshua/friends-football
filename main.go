package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"friends-football/frontend"
	"friends-football/internal/database"
	"friends-football/internal/server"
)

const (
	defaultDBPath    = "./friends-football.db"
	defaultPort      = ":8080"
	selfPingURL      = "https://friends-football.onrender.com/health"
	selfPingInterval = 12 * time.Minute
)

func main() {
	log.Println("Starting Friends Football...")

	// Initialize database
	dbPath := getEnv("DB_PATH", defaultDBPath)
	if err := database.Init(dbPath); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Create and start HTTP server with embedded static files
	port := getEnv("PORT", defaultPort)
	// Render sets PORT as just a number (e.g., "10000"), ensure it has colon prefix
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}
	srv := server.New(server.Config{
		Port:        port,
		StaticFiles: frontend.StaticFiles,
	})

	// Start server in goroutine
	go func() {
		if err := srv.Start(); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Start self-ping worker to keep Render free tier awake
	stopPing := startSelfPing()

	// Set up graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Friends Football is ready")
	log.Printf("Server listening on %s", port)
	log.Printf("Open http://localhost%s in your browser", port)

	// Wait for shutdown signal
	<-quit
	log.Println("Shutting down Friends Football...")

	// Stop self-ping worker
	close(stopPing)

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	if err := database.Close(); err != nil {
		log.Printf("Database close error: %v", err)
	}

	log.Println("Friends Football stopped")
}

// getEnv returns the value of an environment variable or a default value.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// startSelfPing starts a background goroutine that pings the health endpoint
// every 12 minutes to keep the Render free tier awake.
// Returns a channel to stop the worker.
func startSelfPing() chan struct{} {
	stop := make(chan struct{})
	client := &http.Client{Timeout: 10 * time.Second}

	go func() {
		ticker := time.NewTicker(selfPingInterval)
		defer ticker.Stop()

		log.Printf("Self-ping worker started (interval: %v)", selfPingInterval)

		for {
			select {
			case <-ticker.C:
				resp, err := client.Get(selfPingURL)
				if err != nil {
					log.Printf("Self-ping failed: %v", err)
					continue
				}
				resp.Body.Close()
				log.Printf("Self-ping successful (status: %d)", resp.StatusCode)
			case <-stop:
				log.Println("Self-ping worker stopped")
				return
			}
		}
	}()

	return stop
}
