package migrations

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
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
