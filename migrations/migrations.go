package migrations

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/utils"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

const (
	seedSuperAdminEmail    = "super@admin.com"
	seedSuperAdminPassword = "superadmin123"
)

// Migration represents a single database migration.
type Migration struct {
	Version     int
	Description string
	Up          func(ctx context.Context, db *mongo.Database) error
}

// MigrationRecord tracks which migrations have been applied.
type MigrationRecord struct {
	Version     int       `bson:"version"`
	Description string    `bson:"description"`
	AppliedAt   time.Time `bson:"applied_at"`
}

const migrationsCollection = "_migrations"

// GetMigrations returns all defined migrations in order.
func GetMigrations() []Migration {
	return []Migration{
		{
			Version:     1,
			Description: "Create users collection indexes",
			Up: func(ctx context.Context, db *mongo.Database) error {
				collection := db.Collection("users")

				// Create unique index on email
				emailIndex := mongo.IndexModel{
					Keys:    bson.D{{Key: "email", Value: 1}},
					Options: options.Index().SetUnique(true),
				}

				// Create index on created_at for sorting
				createdAtIndex := mongo.IndexModel{
					Keys: bson.D{{Key: "created_at", Value: -1}},
				}

				// Create index on role for filtering
				roleIndex := mongo.IndexModel{
					Keys: bson.D{{Key: "role", Value: 1}},
				}

				_, err := collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
					emailIndex,
					createdAtIndex,
					roleIndex,
				})
				return err
			},
		},
		{
			Version:     2,
			Description: "Create unique sparse index on users.admin_id",
			Up: func(ctx context.Context, db *mongo.Database) error {
				collection := db.Collection("users")

				adminIDIndex := mongo.IndexModel{
					Keys:    bson.D{{Key: "admin_id", Value: 1}},
					Options: options.Index().SetUnique(true).SetSparse(true),
				}

				_, err := collection.Indexes().CreateOne(ctx, adminIDIndex)
				return err
			},
		},
		{
			Version:     3,
			Description: "Seed default super_admin account",
			Up: func(ctx context.Context, db *mongo.Database) error {
				collection := db.Collection("users")

				existing := collection.FindOne(ctx, bson.M{"email": seedSuperAdminEmail})
				if existing.Err() == nil {
					log.Info().Msg("Seed super_admin already exists, skipping")
					return nil
				}

				hashedPassword, err := bcrypt.GenerateFromPassword([]byte(seedSuperAdminPassword), bcrypt.DefaultCost)
				if err != nil {
					return fmt.Errorf("failed to hash seed super_admin password: %w", err)
				}

				adminID, err := utils.GenerateAdminID()
				if err != nil {
					return fmt.Errorf("failed to generate seed super_admin ID: %w", err)
				}

				now := time.Now().UTC()
				superAdmin := domain.User{
					Name:      "Super Admin",
					Email:     seedSuperAdminEmail,
					Password:  string(hashedPassword),
					Role:      domain.RoleSuperAdmin,
					IsActive:  true,
					AdminID:   adminID,
					CreatedAt: now,
					UpdatedAt: now,
				}

				_, err = collection.InsertOne(ctx, superAdmin)
				if err != nil {
					if mongo.IsDuplicateKeyError(err) {
						log.Warn().Msg("Seed super_admin insert hit a duplicate key, skipping")
						return nil
					}
					return fmt.Errorf("failed to seed super_admin: %w", err)
				}

				log.Info().Str("admin_id", adminID).Msg("Seed super_admin created successfully")
				return nil
			},
		},
	}
}

// Run executes all pending migrations.
func Run(ctx context.Context, db *mongo.Database) error {
	migrations := GetMigrations()
	collection := db.Collection(migrationsCollection)

	for _, migration := range migrations {
		// Check if migration has already been applied
		var record MigrationRecord
		err := collection.FindOne(ctx, bson.M{"version": migration.Version}).Decode(&record)
		if err == nil {
			log.Info().
				Int("version", migration.Version).
				Str("description", migration.Description).
				Msg("Migration already applied, skipping")
			continue
		}

		// Apply migration
		log.Info().
			Int("version", migration.Version).
			Str("description", migration.Description).
			Msg("Applying migration")

		if err := migration.Up(ctx, db); err != nil {
			return fmt.Errorf("migration %d failed: %w", migration.Version, err)
		}

		// Record the migration
		_, err = collection.InsertOne(ctx, MigrationRecord{
			Version:     migration.Version,
			Description: migration.Description,
			AppliedAt:   time.Now().UTC(),
		})
		if err != nil {
			return fmt.Errorf("failed to record migration %d: %w", migration.Version, err)
		}

		log.Info().
			Int("version", migration.Version).
			Msg("Migration applied successfully")
	}

	log.Info().Msg("All migrations completed")
	return nil
}
