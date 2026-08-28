package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/config"
	"github.com/smart-invest-solutions/backend/internal/database"
	"github.com/smart-invest-solutions/backend/internal/router"
	"github.com/smart-invest-solutions/backend/migrations"
)

// @title           Smart Invest Solutions API
// @version         1.0
// @description     Backend API for Smart Invest Solutions — a LIC Policy & Investment Management Platform.
// @description     This API provides user registration, authentication with JWT, role-based access control, and more.

// @contact.name    ULMIND Team
// @contact.email   support@ulmind.com

// @BasePath        /api/v1

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Enter your Bearer token in the format: **Bearer &lt;token&gt;**

func main() {
	// Load configuration
	cfg := config.Load()

	// Setup logger
	setupLogger(cfg)

	log.Info().Msg("Starting Smart Invest Solutions Backend...")

	// Connect to MongoDB
	db, err := database.Connect(cfg.MongoDBURI, cfg.DBName)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to MongoDB")
	}

	// Run database migrations
	ctx := context.Background()
	if err := migrations.Run(ctx, db.Database, cfg); err != nil {
		log.Fatal().Err(err).Msg("Failed to run database migrations")
	}

	// Setup router
	r := router.Setup(db, cfg)

	// Create HTTP server
	addr := fmt.Sprintf(":%s", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Info().Str("address", addr).Msg("Server is running")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server failed to start")
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down server...")

	// Give outstanding requests 5 seconds to complete
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatal().Err(err).Msg("Server forced to shutdown")
	}

	// Disconnect from MongoDB
	if err := db.Disconnect(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Error disconnecting from MongoDB")
	}

	log.Info().Msg("Server exited gracefully")
}

// setupLogger configures zerolog based on the environment.
func setupLogger(cfg *config.Config) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	if cfg.IsDevelopment() {
		// Human-readable console output for development
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		// JSON output for production
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}
