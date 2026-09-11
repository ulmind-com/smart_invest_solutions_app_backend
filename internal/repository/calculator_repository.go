package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type calculatorSettingsRepository struct {
	collection *mongo.Collection
}

// singletonSettingsID is a fixed, well-known ObjectID (the zero value) used as the _id of the one
// and only calculator settings document. Querying/upserting by this fixed _id — rather than by an
// empty filter bson.M{} — makes MongoDB's own unique index on _id the concurrency guard: two
// simultaneous "no settings yet" upserts can no longer both succeed and create two documents,
// which previously could leave GetSettings non-deterministically returning either one.
var singletonSettingsID = bson.ObjectID{}

// NewCalculatorSettingsRepository initializes a new CalculatorSettingsRepository.
func NewCalculatorSettingsRepository(db *mongo.Database) domain.CalculatorSettingsRepository {
	return &calculatorSettingsRepository{
		collection: db.Collection("calculator_settings"),
	}
}

// GetSettings retrieves the global calculator default settings.
// If no settings exist yet, it automatically seeds default rates (SIP: 12%, Lumpsum: 12%, FD: 7%).
func (r *calculatorSettingsRepository) GetSettings(ctx context.Context) (*domain.CalculatorSettings, error) {
	var settings domain.CalculatorSettings
	err := r.collection.FindOne(ctx, bson.M{"_id": singletonSettingsID}).Decode(&settings)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// Seed default settings on initial access
			defaultSettings := &domain.CalculatorSettings{
				DefaultSIPRate:     12.0,
				DefaultLumpsumRate: 12.0,
				DefaultFDRate:      7.0,
				UpdatedAt:          time.Now().UTC(),
			}
			return r.UpsertSettings(ctx, defaultSettings)
		}
		return nil, fmt.Errorf("failed to fetch calculator settings: %w", err)
	}
	return &settings, nil
}

// UpsertSettings creates or updates the single global calculator settings document.
func (r *calculatorSettingsRepository) UpsertSettings(ctx context.Context, settings *domain.CalculatorSettings) (*domain.CalculatorSettings, error) {
	now := time.Now().UTC()
	settings.UpdatedAt = now

	update := bson.M{
		"$set": bson.M{
			"default_sip_rate":     settings.DefaultSIPRate,
			"default_lumpsum_rate": settings.DefaultLumpsumRate,
			"default_fd_rate":      settings.DefaultFDRate,
			"updated_at":           now,
		},
	}

	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var updated domain.CalculatorSettings
	err := r.collection.FindOneAndUpdate(ctx, bson.M{"_id": singletonSettingsID}, update, opts).Decode(&updated)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert calculator settings: %w", err)
	}

	return &updated, nil
}
