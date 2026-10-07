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
		{
			Version:     9,
			Description: "Make referrals staff-only and drop client validity dates",
			Up:          moveReferralsToStaff,
		},
		{
			Version:     10,
			Description: "Stamp who manages each existing policy, deposit and motor record",
			Up:          backfillManagedBy,
		},
		{
			Version:     11,
			Description: "Index users by agency for the client map",
			Up: func(ctx context.Context, db *mongo.Database) error {
				// The client map scopes on role + agency before joining anything, so this compound
				// index is what keeps a super admin's platform-wide view and an admin's own-agency
				// view off a collection scan. The holdings collections already index user_id, which
				// is what the map's count lookups join on.
				_, err := db.Collection("users").Indexes().CreateOne(ctx, mongo.IndexModel{
					Keys: bson.D{{Key: "role", Value: 1}, {Key: "agency_id", Value: 1}},
				})
				return err
			},
		},
		{
			Version:     12,
			Description: "Retire the separate referral code — the Agency ID is the only code",
			Up:          retireReferralCodes,
		},
	}
}

// retireReferralCodes removes the separate advisor referral code from the platform.
//
// An admin's Admin ID has always *been* their Agency ID, and a client typing it already filed them
// under that admin. The referral code was a second value that did the same job, which meant two boxes
// on the signup form where one would do, and an applicant with no way to know which mattered. From
// here on the Agency ID is the only code: typing it files the client under that agency and credits
// that admin with bringing them in.
//
// Referral records already filed are left exactly as they are — they point at the referrer's account,
// not at a code, so past attribution survives this and the super admin's report keeps its history.
func retireReferralCodes(ctx context.Context, db *mongo.Database) error {
	users := db.Collection("users")

	// Drop the index before the field: a unique index left behind on a field nothing writes any more
	// is a trap for whoever next adds a column with that name.
	if err := users.Indexes().DropOne(ctx, "referral_code_1"); err != nil {
		// Absent on a database that never reached migration 9, which is not a problem to solve here.
		log.Info().Err(err).Msg("No referral_code index to drop")
	}

	if _, err := users.UpdateMany(ctx,
		bson.M{"referral_code": bson.M{"$exists": true}},
		bson.M{"$unset": bson.M{"referral_code": ""}},
	); err != nil {
		return fmt.Errorf("failed to remove referral codes from accounts: %w", err)
	}

	// The code an applicant typed, stored on their access request. The Agency ID it resolved to is
	// kept in applied_agency_id, so nothing about who the request belongs to is lost.
	if _, err := db.Collection("access_requests").UpdateMany(ctx,
		bson.M{"applied_referral_code": bson.M{"$exists": true}},
		bson.M{"$unset": bson.M{"applied_referral_code": ""}},
	); err != nil {
		return fmt.Errorf("failed to remove applied referral codes from access requests: %w", err)
	}

	return nil
}

// backfillManagedBy fills in the managed_by flag the client app now shows on every holding.
//
// Records created from here on are stamped from the role of whoever files them. For records that
// already exist there is no stored provenance, so the agency's own mapping flag is used as the best
// available signal: is_mapped means an admin put it in the agency portfolio, so the agency manages
// it; everything else is treated as client-added. Motor policies have no mapping flag and can only
// be created by clients today, so they are all client-managed. An admin can correct any record from
// its edit form.
func backfillManagedBy(ctx context.Context, db *mongo.Database) error {
	mappedCollections := []string{"life_insurances", "health_insurances", "fixed_deposits"}

	for _, name := range mappedCollections {
		collection := db.Collection(name)

		if _, err := collection.UpdateMany(ctx,
			bson.M{"managed_by": bson.M{"$exists": false}, "is_mapped": true},
			bson.M{"$set": bson.M{"managed_by": domain.ManagedByAgency}},
		); err != nil {
			return fmt.Errorf("failed to mark agency-managed records in %s: %w", name, err)
		}

		if _, err := collection.UpdateMany(ctx,
			bson.M{"managed_by": bson.M{"$exists": false}},
			bson.M{"$set": bson.M{"managed_by": domain.ManagedByClient}},
		); err != nil {
			return fmt.Errorf("failed to mark client-managed records in %s: %w", name, err)
		}
	}

	if _, err := db.Collection("general_insurances").UpdateMany(ctx,
		bson.M{"managed_by": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"managed_by": domain.ManagedByClient}},
	); err != nil {
		return fmt.Errorf("failed to mark client-managed motor policies: %w", err)
	}

	return nil
}

// moveReferralsToStaff migrates the two rules that changed together:
//
//   - A client account no longer expires, so every app_validity_end_date is dropped. (Admin access
//     still expires — that lives in admin_expiry_date and is untouched.)
//   - Referral codes stored on client accounts are removed. (Staff codes are no longer issued here
//     at all — see migration 12, which retires the separate code in favour of the Agency ID.)
//     Referral records already filed keep pointing at whoever made them, so past attribution isn't
//     rewritten.
func moveReferralsToStaff(ctx context.Context, db *mongo.Database) error {
	users := db.Collection("users")

	if _, err := users.UpdateMany(ctx,
		bson.M{"app_validity_end_date": bson.M{"$exists": true}},
		bson.M{"$unset": bson.M{"app_validity_end_date": ""}},
	); err != nil {
		return fmt.Errorf("failed to drop client validity dates: %w", err)
	}

	// This migration used to also issue every admin a separate referral code. Migration 12 retires
	// those codes entirely — an admin's Agency ID (their Admin ID) is now the only code anyone
	// shares — so issuing them here would only create data the next migration deletes. Codes already
	// issued on a database that ran this earlier are cleaned up there.
	if _, err := users.UpdateMany(ctx,
		bson.M{
			"role":          bson.M{"$in": bson.A{domain.RoleClient, domain.RoleAdvisor}},
			"referral_code": bson.M{"$exists": true},
		},
		bson.M{"$unset": bson.M{"referral_code": ""}},
	); err != nil {
		return fmt.Errorf("failed to remove client referral codes: %w", err)
	}

	return nil
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
