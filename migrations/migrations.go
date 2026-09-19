package migrations

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/config"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/utils"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/crypto/bcrypt"
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

// GetMigrations returns all defined migrations in order. The super_admin seed
// credentials are sourced from cfg (env-configurable) rather than being hardcoded.
func GetMigrations(cfg *config.Config) []Migration {
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

				existing := collection.FindOne(ctx, bson.M{"email": cfg.SuperAdminEmail})
				if existing.Err() == nil {
					log.Info().Msg("Seed super_admin already exists, skipping")
					return nil
				}

				hashedPassword, err := bcrypt.GenerateFromPassword([]byte(cfg.SuperAdminPassword), bcrypt.DefaultCost)
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
					Email:     cfg.SuperAdminEmail,
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
		{
			Version:     5,
			Description: "Create sparse index on users.admin_expiry_date",
			Up: func(ctx context.Context, db *mongo.Database) error {
				collection := db.Collection("users")

				// Sparse because most accounts (clients, super_admins, and legacy admins created
				// before this feature existed) never set this field.
				expiryIndex := mongo.IndexModel{
					Keys:    bson.D{{Key: "admin_expiry_date", Value: 1}},
					Options: options.Index().SetSparse(true),
				}

				_, err := collection.Indexes().CreateOne(ctx, expiryIndex)
				return err
			},
		},
		{
			Version:     6,
			Description: "Create sparse indexes for the Agency ID multi-tenancy feature",
			Up: func(ctx context.Context, db *mongo.Database) error {
				usersCollection := db.Collection("users")
				usersIndex := mongo.IndexModel{
					Keys:    bson.D{{Key: "agency_id", Value: 1}},
					Options: options.Index().SetSparse(true),
				}
				if _, err := usersCollection.Indexes().CreateOne(ctx, usersIndex); err != nil {
					return err
				}

				accessRequestsCollection := db.Collection("access_requests")
				accessRequestsIndex := mongo.IndexModel{
					Keys:    bson.D{{Key: "applied_agency_id", Value: 1}},
					Options: options.Index().SetSparse(true),
				}
				_, err := accessRequestsCollection.Indexes().CreateOne(ctx, accessRequestsIndex)
				return err
			},
		},
		{
			Version:     7,
			Description: "Create sparse index on family_members.lic_customer_id",
			Up: func(ctx context.Context, db *mongo.Database) error {
				collection := db.Collection("family_members")

				// Sparse because most family members never have this set — only relevant when
				// they've bought a policy from an insurer (LIC or otherwise) that issued them one.
				licIndex := mongo.IndexModel{
					Keys:    bson.D{{Key: "lic_customer_id", Value: 1}},
					Options: options.Index().SetSparse(true),
				}

				_, err := collection.Indexes().CreateOne(ctx, licIndex)
				return err
			},
		},
		{
			Version:     8,
			Description: "Replace the placeholder advisor on motor policies with the client's agency admin",
			Up:          backfillMotorPolicyAdvisors,
		},
	}
}

// legacyAdvisorContact is the dummy number every motor policy used to be stamped with, whatever
// agency the client belonged to.
const legacyAdvisorContact = "+91 9876543210"

// backfillMotorPolicyAdvisors rewrites motor policies still carrying the placeholder advisor: a
// client with an agency gets that agency admin's name and phone; an unassigned client just loses the
// fake number (the app then shows no call button rather than dialling a stranger).
func backfillMotorPolicyAdvisors(ctx context.Context, db *mongo.Database) error {
	policies := db.Collection("general_insurances")
	users := db.Collection("users")

	cursor, err := policies.Find(ctx, bson.M{"advisor_contact": legacyAdvisorContact},
		options.Find().SetProjection(bson.M{"_id": 1, "user_id": 1}))
	if err != nil {
		return fmt.Errorf("failed to find motor policies with the placeholder advisor: %w", err)
	}
	defer cursor.Close(ctx)

	advisors := map[bson.ObjectID][2]string{} // user_id -> {name, phone}
	for cursor.Next(ctx) {
		var row struct {
			ID     bson.ObjectID `bson:"_id"`
			UserID bson.ObjectID `bson:"user_id"`
		}
		if err := cursor.Decode(&row); err != nil {
			continue
		}

		advisor, cached := advisors[row.UserID]
		if !cached {
			var client domain.User
			if err := users.FindOne(ctx, bson.M{"_id": row.UserID}).Decode(&client); err == nil && client.AgencyID != "" {
				var admin domain.User
				if err := users.FindOne(ctx, bson.M{"admin_id": client.AgencyID}).Decode(&admin); err == nil {
					advisor = [2]string{admin.Name, admin.Phone}
				}
			}
			advisors[row.UserID] = advisor
		}

		set := bson.M{"advisor_contact": advisor[1], "updated_at": time.Now().UTC()}
		if advisor[0] != "" {
			set["advisor_name"] = advisor[0]
		}
		if _, err := policies.UpdateOne(ctx, bson.M{"_id": row.ID}, bson.M{"$set": set}); err != nil {
			return fmt.Errorf("failed to update motor policy %s: %w", row.ID.Hex(), err)
		}
	}
	return cursor.Err()
}

// Run executes all pending migrations.
func Run(ctx context.Context, db *mongo.Database, cfg *config.Config) error {
	migrations := GetMigrations(cfg)
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
