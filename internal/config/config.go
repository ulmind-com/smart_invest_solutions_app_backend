package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

// defaultJWTSecret is the placeholder JWT secret used when none is configured.
// It must never be allowed to reach a production deployment — see Load().
const defaultJWTSecret = "default-secret-change-me"

// Config holds all configuration for the application.
type Config struct {
	MongoDBURI         string
	DBName             string
	Port               string
	Env                string
	JWTSecret          string
	JWTExpiryHours     string
	CloudinaryURL      string
	MailAddress        string
	ResendAPIKey       string
	SuperAdminEmail    string
	SuperAdminPassword string
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
		MongoDBURI:         getEnv("MONGODB_URI", "mongodb://localhost:27017"),
		DBName:             getEnv("DB_NAME", "smart_invest_solutions"),
		Port:               getEnv("PORT", "8080"),
		Env:                getEnv("ENV", "development"),
		JWTSecret:          getEnv("JWT_SECRET", defaultJWTSecret),
		JWTExpiryHours:     getEnv("JWT_EXPIRY_HOURS", "24"),
		CloudinaryURL:      getEnv("CLOUDINARY_URL", ""),
		MailAddress:        getEnv("MAIL_ADDRESS", "noreply@samiransamanta.in"),
		ResendAPIKey:       getEnv("RESEND_API_KEY", ""),
		SuperAdminEmail:    getEnv("SUPER_ADMIN_EMAIL", "super@admin.com"),
		SuperAdminPassword: getEnv("SUPER_ADMIN_PASSWORD", "superadmin123"),
	}

	if cfg.IsProduction() {
		if _, ok := os.LookupEnv("SUPER_ADMIN_EMAIL"); !ok {
			log.Println("WARNING: SUPER_ADMIN_EMAIL not set in production — falling back to the default seed email. Set it and rotate the account after first login.")
		}
		if _, ok := os.LookupEnv("SUPER_ADMIN_PASSWORD"); !ok {
			log.Println("WARNING: SUPER_ADMIN_PASSWORD not set in production — falling back to the default seed password. Set it and rotate the account after first login.")
		}
		if cfg.JWTSecret == "" || cfg.JWTSecret == defaultJWTSecret {
			log.Fatal("FATAL: JWT_SECRET must be set to a strong, unique value in production (refusing to start with the default placeholder)")
		}
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
