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

type emailVerificationRepository struct {
	collection *mongo.Collection
}

// NewEmailVerificationRepository initializes a new EmailVerificationRepository.
func NewEmailVerificationRepository(db *mongo.Database) domain.EmailVerificationRepository {
	col := db.Collection("email_verifications")

	// Index for fast email lookup
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "email", Value: 1},
			{Key: "is_used", Value: 1},
			{Key: "created_at", Value: -1},
		},
	})

	// TTL Index on expires_at to automatically purge expired OTP documents
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(0),
	})

	return &emailVerificationRepository{collection: col}
}

// Create inserts a new email verification OTP record into MongoDB.
func (r *emailVerificationRepository) Create(ctx context.Context, verif *domain.EmailVerification) (*domain.EmailVerification, error) {
	now := time.Now().UTC()
	verif.CreatedAt = now

	result, err := r.collection.InsertOne(ctx, verif)
	if err != nil {
		return nil, fmt.Errorf("failed to create email verification record: %w", err)
	}

	verif.ID = result.InsertedID.(bson.ObjectID)
	return verif, nil
}

// FindLatestActiveOTP retrieves the most recently issued, unexpired, and unused OTP record for an email.
func (r *emailVerificationRepository) FindLatestActiveOTP(ctx context.Context, email string) (*domain.EmailVerification, error) {
	filter := bson.M{
		"email":      email,
		"is_used":    false,
		"expires_at": bson.M{"$gt": time.Now().UTC()},
	}
	opts := options.FindOne().SetSort(bson.D{{Key: "created_at", Value: -1}})

	var verif domain.EmailVerification
	err := r.collection.FindOne(ctx, filter, opts).Decode(&verif)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // No active OTP found
		}
		return nil, fmt.Errorf("failed to query email verification record: %w", err)
	}

	return &verif, nil
}

// IncrementAttempts increments the failed attempt count for an OTP record.
func (r *emailVerificationRepository) IncrementAttempts(ctx context.Context, id bson.ObjectID) error {
	filter := bson.M{"_id": id}
	update := bson.M{"$inc": bson.M{"attempts": 1}}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to increment OTP attempts: %w", err)
	}
	return nil
}

// MarkAsUsed sets is_used = true on an OTP record.
func (r *emailVerificationRepository) MarkAsUsed(ctx context.Context, id bson.ObjectID) error {
	filter := bson.M{"_id": id}
	update := bson.M{"$set": bson.M{"is_used": true}}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to mark OTP as used: %w", err)
	}
	return nil
}

// DeleteAllByEmail removes all verification records for an email address.
func (r *emailVerificationRepository) DeleteAllByEmail(ctx context.Context, email string) error {
	filter := bson.M{"email": email}
	_, err := r.collection.DeleteMany(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to delete email verifications: %w", err)
	}
	return nil
}
