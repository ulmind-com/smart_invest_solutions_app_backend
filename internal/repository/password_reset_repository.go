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

type passwordResetRepository struct {
	collection *mongo.Collection
}

// NewPasswordResetRepository creates a new instance of PasswordResetRepository.
func NewPasswordResetRepository(db *mongo.Database) domain.PasswordResetRepository {
	col := db.Collection("password_resets")

	// Ensure index on email and expires_at for fast lookups
	_, _ = col.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "email", Value: 1},
				{Key: "otp", Value: 1},
				{Key: "is_used", Value: 1},
			},
		},
	})

	return &passwordResetRepository{
		collection: col,
	}
}

// Create inserts a new OTP record into MongoDB.
func (r *passwordResetRepository) Create(ctx context.Context, reset *domain.PasswordReset) (*domain.PasswordReset, error) {
	reset.CreatedAt = time.Now().UTC()

	result, err := r.collection.InsertOne(ctx, reset)
	if err != nil {
		return nil, fmt.Errorf("failed to save OTP: %w", err)
	}

	reset.ID = result.InsertedID.(bson.ObjectID)
	return reset, nil
}

// FindLatestActiveOTP finds the most recent unexpired, unused OTP for a given email and code.
func (r *passwordResetRepository) FindLatestActiveOTP(ctx context.Context, email, otp string) (*domain.PasswordReset, error) {
	filter := bson.M{
		"email":      email,
		"otp":        otp,
		"is_used":    false,
		"expires_at": bson.M{"$gt": time.Now().UTC()},
	}

	opts := options.FindOne().SetSort(bson.D{{Key: "created_at", Value: -1}})

	var reset domain.PasswordReset
	err := r.collection.FindOne(ctx, filter, opts).Decode(&reset)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("invalid or expired OTP code")
		}
		return nil, fmt.Errorf("failed to query OTP: %w", err)
	}

	return &reset, nil
}

// MarkAsUsed sets is_used = true for an OTP record.
func (r *passwordResetRepository) MarkAsUsed(ctx context.Context, id bson.ObjectID) error {
	filter := bson.M{"_id": id}
	update := bson.M{"$set": bson.M{"is_used": true}}

	_, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to invalidate OTP: %w", err)
	}
	return nil
}
