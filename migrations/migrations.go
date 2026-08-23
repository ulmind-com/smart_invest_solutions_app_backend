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

				pin, err := utils.GenerateNumericCode(4)
				if err != nil {
					return fmt.Errorf("failed to generate seed super_admin PIN: %w", err)
				}
				hashedPIN, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
				if err != nil {
					return fmt.Errorf("failed to hash seed super_admin PIN: %w", err)
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
					PIN:       string(hashedPIN),
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

				log.Warn().Str("admin_id", adminID).Str("pin", pin).Msg("Seed super_admin created successfully — record this PIN now, it cannot be retrieved again")
				return nil
			},
		},
		{
			Version:     4,
			Description: "Backfill PIN for admin/super_admin accounts missing one",
			Up: func(ctx context.Context, db *mongo.Database) error {
				collection := db.Collection("users")

				filter := bson.M{
					"role": bson.M{"$in": []string{domain.RoleAdmin, domain.RoleSuperAdmin}},
					"$or": []bson.M{
						{"pin": bson.M{"$exists": false}},
						{"pin": ""},
					},
				}

				cursor, err := collection.Find(ctx, filter)
				if err != nil {
					return fmt.Errorf("failed to find admin accounts missing a PIN: %w", err)
				}
				defer cursor.Close(ctx)

				var accounts []domain.User
				if err := cursor.All(ctx, &accounts); err != nil {
					return fmt.Errorf("failed to decode admin accounts: %w", err)
				}

				for _, account := range accounts {
					pin, err := utils.GenerateNumericCode(4)
					if err != nil {
						return fmt.Errorf("failed to generate backfill PIN for %s: %w", account.Email, err)
					}
					hashedPIN, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
					if err != nil {
						return fmt.Errorf("failed to hash backfill PIN for %s: %w", account.Email, err)
					}

					update := bson.M{"$set": bson.M{"pin": string(hashedPIN), "updated_at": time.Now().UTC()}}
					if _, err := collection.UpdateOne(ctx, bson.M{"_id": account.ID}, update); err != nil {
						return fmt.Errorf("failed to backfill PIN for %s: %w", account.Email, err)
					}

					log.Warn().
						Str("email", account.Email).
						Str("admin_id", account.AdminID).
						Str("pin", pin).
						Msg("Backfilled a new PIN for an existing admin account that had none — record this PIN now and have the admin change it after logging in")
				}

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
