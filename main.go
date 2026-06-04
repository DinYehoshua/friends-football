package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"friends-football/frontend"
	"friends-football/internal/database"
	"friends-football/internal/server"
)

const (
	defaultDBPath = "./friends-football.db"
	defaultPort   = ":8080"
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

	// Set up graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Friends Football is ready")
	log.Printf("Server listening on %s", port)
	log.Printf("Open http://localhost%s in your browser", port)

	// Wait for shutdown signal
	<-quit
	log.Println("Shutting down Friends Football...")

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
