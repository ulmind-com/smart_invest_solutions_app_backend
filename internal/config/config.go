package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the application.
type Config struct {
	MongoDBURI     string
	DBName         string
	Port           string
	Env            string
	JWTSecret      string
	JWTExpiryHours string
}

// Load reads configuration from environment variables.
// It attempts to load a .env file but does not fail if one is not found,
// allowing environment variables to be set by the deployment environment.
func Load() *Config {
	// Load .env file if it exists (non-fatal if missing)
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, reading configuration from environment")
	}

	cfg := &Config{
		MongoDBURI:     getEnv("MONGODB_URI", "mongodb://localhost:27017"),
		DBName:         getEnv("DB_NAME", "smart_invest_solutions"),
		Port:           getEnv("PORT", "8080"),
		Env:            getEnv("ENV", "development"),
		JWTSecret:      getEnv("JWT_SECRET", "default-secret-change-me"),
		JWTExpiryHours: getEnv("JWT_EXPIRY_HOURS", "24"),
	}

	return cfg
}

// IsDevelopment returns true if the application is running in development mode.
func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

// IsProduction returns true if the application is running in production mode.
func (c *Config) IsProduction() bool {
	return c.Env == "production"
}

// getEnv retrieves an environment variable or returns a default value.
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
