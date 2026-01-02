package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nsilverman/archivist/internal/api"
	"github.com/nsilverman/archivist/internal/config"
	"github.com/nsilverman/archivist/internal/executor"
	"github.com/nsilverman/archivist/internal/scheduler"
	"github.com/nsilverman/archivist/internal/storage"
)

const (
	defaultPort    = "8080"
	defaultRootDir = "/data"
)

func main() {
	// Parse command line flags
	port := flag.String("port", getEnv("ARCHIVIST_PORT", defaultPort), "HTTP server port")
	rootDir := flag.String("root", getEnv("ARCHIVIST_ROOT", defaultRootDir), "Root data directory")
	logLevel := flag.String("log-level", getEnv("ARCHIVIST_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	flag.Parse()

	// Derive paths from root directory
	configPath := filepath.Join(*rootDir, "config", "config.json")
	dbPath := filepath.Join(*rootDir, "config", "archivist.db")
	tempDir := filepath.Join(*rootDir, "temp")
	sourcesDir := filepath.Join(*rootDir, "sources")

	// Setup logging
	setupLogging(*logLevel)

	log.Println("Starting Archivist...")
	log.Printf("Version: %s", getVersion())
	log.Printf("Root directory: %s", *rootDir)
	log.Printf("Config: %s", configPath)
	log.Printf("Database: %s", dbPath)

	// Ensure required directories exist
	if err := ensureDirectories(*rootDir, tempDir, sourcesDir); err != nil {
		log.Fatalf("Failed to create directories: %v", err)
	}

	// Initialize configuration manager
	configMgr, err := config.NewManager(configPath, *rootDir)
	if err != nil {
		log.Fatalf("Failed to initialize configuration manager: %v", err)
	}

	// Load or create default configuration
	if err := configMgr.Load(); err != nil {
		if os.IsNotExist(err) {
			log.Println("No configuration file found, creating default configuration...")
			if err := configMgr.CreateDefaultWithPaths(tempDir, sourcesDir); err != nil {
				log.Fatalf("Failed to create default configuration: %v", err)
			}
			log.Println("Default configuration created")
		} else {
			log.Fatalf("Failed to load configuration: %v", err)
		}
	}
	log.Println("Configuration loaded")

	// Initialize database
	log.Println("Initializing database...")
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Error closing database: %v", err)
		}
	}()
	log.Println("Database initialized")

	// Initialize backup executor
	log.Println("Initializing executor...")
	exec := executor.NewExecutor(configMgr, db)
	log.Println("Executor initialized")

	// Initialize scheduler
	log.Println("Initializing scheduler...")
	sched := scheduler.NewScheduler(exec, configMgr)
	if err := sched.Start(); err != nil {
		log.Fatalf("Failed to start scheduler: %v", err)
	}
	defer sched.Stop()
	log.Println("Scheduler started")

	// Initialize API server
	log.Println("Initializing API server...")
	server := api.NewServer(configMgr, db, exec, sched)
	log.Println("API server initialized")
	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%s", *port),
		Handler:      server.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start HTTP server in a goroutine
	go func() {
		log.Printf("HTTP server listening on port %s", *port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped")
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// ensureDirectories creates required directories if they don't exist
func ensureDirectories(rootDir, tempDir, sourcesDir string) error {
	dirs := []string{
		filepath.Join(rootDir, "config"),
		tempDir,
		sourcesDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	log.Printf("Ensured directories exist: config, temp, sources")
	return nil
}

// setupLogging configures the logging based on the log level
func setupLogging(level string) {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	// In a more complete implementation, we would set up structured logging
	// with proper levels using a library like logrus or zap
}

// getVersion returns the application version
func getVersion() string {
	// This would typically be injected at build time using ldflags
	return "1.0.0-dev"
}
