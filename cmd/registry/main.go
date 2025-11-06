package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/registry/internal/api"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/aws"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/importer"
	"github.com/modelcontextprotocol/registry/internal/service"
	"github.com/modelcontextprotocol/registry/internal/telemetry"
)

// Version info for the MCP Registry application
// These variables are injected at build time via ldflags
var (
	// Version is the current version of the MCP Registry application
	Version = "dev"

	// BuildTime is the time at which the binary was built
	BuildTime = "unknown"

	// GitCommit is the git commit that was compiled
	GitCommit = "unknown"
)

func main() {
	// Parse command line flags
	showVersion := flag.Bool("version", false, "Display version information")
	flag.Parse()

	// Show version information if requested
	if *showVersion {
		log.Printf("MCP Registry %s\n", Version)
		log.Printf("Git commit: %s\n", GitCommit)
		log.Printf("Build time: %s\n", BuildTime)
		return
	}

	log.Printf("Starting MCP Registry Application v%s (commit: %s)", Version, GitCommit)

	var (
		registryService service.RegistryService
		db              database.Database
		jsonDB          *database.JSONFileDB
		sqsListener     *aws.SQSListener
		err             error
	)

	// Initialize configuration
	cfg := config.NewConfig()

	// Create a context with timeout for database connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Connect to database based on DatabaseType
	switch cfg.DatabaseType {
	case "jsonfile":
		log.Printf("Using JSON file database at %s", cfg.JSONFilePath)
		jsonDB, err = database.NewJSONFileDB(ctx, cfg.JSONFilePath)
		if err != nil {
			log.Printf("Failed to initialize JSON file database: %v", err)
			return
		}
		db = jsonDB
	case "postgres":
		log.Printf("Using PostgreSQL database")
		db, err = database.NewPostgreSQL(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Printf("Failed to connect to PostgreSQL: %v", err)
			return
		}
	default:
		log.Printf("Invalid database type: %s (must be 'postgres' or 'jsonfile')", cfg.DatabaseType)
		return
	}

	// Store the database instance for later cleanup
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Error closing database connection: %v", err)
		} else {
			log.Println("Database connection closed successfully")
		}
	}()

	registryService = service.NewRegistryService(db, cfg)

	// Import seed data if seed source is provided
	if cfg.SeedFrom != "" {
		log.Printf("Importing data from %s...", cfg.SeedFrom)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		importerService := importer.NewService(registryService)
		if err := importerService.ImportFromPath(ctx, cfg.SeedFrom); err != nil {
			log.Printf("Failed to import seed data: %v", err)
		}
	}

	// Initialize SQS listener if enabled and using JSON file database
	if cfg.SQSEnabled && cfg.DatabaseType == "jsonfile" {
		if cfg.SQSQueueURL == "" {
			log.Printf("SQS is enabled but SQS_QUEUE_URL is not configured")
		} else if jsonDB == nil {
			log.Printf("SQS listener can only be used with JSON file database")
		} else {
			log.Printf("Initializing SQS listener for queue: %s", cfg.SQSQueueURL)

			// Create context for SQS listener
			sqsCtx := context.Background()

			sqsListener, err = aws.NewSQSListener(sqsCtx, aws.SQSListenerConfig{
				QueueURL:       cfg.SQSQueueURL,
				TargetFilePath: cfg.JSONFilePath,
				ReloadCallback: func() error {
					return jsonDB.Reload()
				},
				MaxMessages:     1,
				WaitTimeSeconds: 20,
			})
			if err != nil {
				log.Printf("Failed to initialize SQS listener: %v", err)
			} else {
				// Start the listener
				sqsListener.Start(sqsCtx)
				log.Printf("SQS listener started successfully")
			}
		}
	}

	shutdownTelemetry, metrics, err := telemetry.InitMetrics(cfg.Version)
	if err != nil {
		log.Printf("Failed to initialize metrics: %v", err)
		return
	}

	defer func() {
		if err := shutdownTelemetry(context.Background()); err != nil {
			log.Printf("Failed to shutdown telemetry: %v", err)
		}
	}()

	// Prepare version information
	versionInfo := &v0.VersionBody{
		Version:   Version,
		GitCommit: GitCommit,
		BuildTime: BuildTime,
	}

	// Initialize HTTP server
	server := api.NewServer(cfg, registryService, metrics, versionInfo)

	// Start server in a goroutine so it doesn't block signal handling
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Failed to start server: %v", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)

	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Stop SQS listener if running
	if sqsListener != nil {
		sqsListener.Stop()
	}

	// Create context with timeout for shutdown
	sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scancel()

	// Gracefully shutdown the server
	if err := server.Shutdown(sctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exiting")
}
