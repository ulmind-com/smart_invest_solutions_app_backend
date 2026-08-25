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

type referralRepository struct {
	collection *mongo.Collection
}

// NewReferralRepository initializes a new ReferralRepository.
func NewReferralRepository(db *mongo.Database) domain.ReferralRepository {
	col := db.Collection("referral_records")

	// Index on referrer_id and status for fast stats queries
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "referrer_id", Value: 1},
			{Key: "status", Value: 1},
		},
	})

	// Index on referred_email for fast lookup on access request approval
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "referred_email", Value: 1}},
	})

	return &referralRepository{collection: col}
}

// Create inserts a new referral record into MongoDB.
func (r *referralRepository) Create(ctx context.Context, record *domain.ReferralRecord) (*domain.ReferralRecord, error) {
	now := time.Now().UTC()
	record.CreatedAt = now
	record.UpdatedAt = now

	result, err := r.collection.InsertOne(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("failed to create referral record: %w", err)
	}

	record.ID = result.InsertedID.(bson.ObjectID)
	return record, nil
}

// GetPendingByReferredEmail retrieves a pending referral record matching a referred email.
func (r *referralRepository) GetPendingByReferredEmail(ctx context.Context, email string) (*domain.ReferralRecord, error) {
	filter := bson.M{
		"referred_email": email,
		"status":         domain.ReferralStatusPending,
	}

	var record domain.ReferralRecord
	err := r.collection.FindOne(ctx, filter).Decode(&record)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // No pending referral found
		}
		return nil, fmt.Errorf("failed to query pending referral record: %w", err)
	}

	return &record, nil
}

// UpdateStatus updates the status and reward days credited on a referral record.
func (r *referralRepository) UpdateStatus(ctx context.Context, id bson.ObjectID, status string, rewardDays int) error {
	filter := bson.M{"_id": id}
	update := bson.M{
		"$set": bson.M{
			"status":               status,
			"reward_days_credited": rewardDays,
			"updated_at":           time.Now().UTC(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update referral record status: %w", err)
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("referral record not found")
	}

	return nil
}

// GetByReferrerID retrieves all referral records created by a specific referrer.
func (r *referralRepository) GetByReferrerID(ctx context.Context, referrerID bson.ObjectID) ([]*domain.ReferralRecord, error) {
	filter := bson.M{"referrer_id": referrerID}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to query referral records by referrer: %w", err)
	}
	defer cursor.Close(ctx)

	var records []*domain.ReferralRecord
	if err := cursor.All(ctx, &records); err != nil {
		return nil, fmt.Errorf("failed to decode referral records: %w", err)
	}

	if records == nil {
		records = []*domain.ReferralRecord{}
	}

	return records, nil
}

// GetStatsByReferrerID calculates referral summary metrics for a referrer.
func (r *referralRepository) GetStatsByReferrerID(ctx context.Context, referrerID bson.ObjectID) (totalPending int64, totalCompleted int64, totalDays int64, err error) {
	filterPending := bson.M{"referrer_id": referrerID, "status": domain.ReferralStatusPending}
	totalPending, err = r.collection.CountDocuments(ctx, filterPending)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to count pending referrals: %w", err)
	}

	filterCompleted := bson.M{"referrer_id": referrerID, "status": domain.ReferralStatusCompleted}
	totalCompleted, err = r.collection.CountDocuments(ctx, filterCompleted)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to count completed referrals: %w", err)
	}

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.D{{Key: "referrer_id", Value: referrerID}, {Key: "status", Value: domain.ReferralStatusCompleted}}}},
		bson.D{{Key: "$group", Value: bson.D{{Key: "_id", Value: nil}, {Key: "total_days", Value: bson.D{{Key: "$sum", Value: "$reward_days_credited"}}}}}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err == nil && cursor.Next(ctx) {
		var result struct {
			TotalDays int64 `bson:"total_days"`
		}
		if err := cursor.Decode(&result); err == nil {
			totalDays = result.TotalDays
		}
		cursor.Close(ctx)
	}

	return totalPending, totalCompleted, totalDays, nil
}

// GetAll retrieves a paginated master list of all referral records across all clients,
// enriched (via $lookup on users collection) with ReferrerName and ReferrerEmail for Admin tracking.
func (r *referralRepository) GetAll(ctx context.Context, page, limit int64) ([]*domain.ReferralRecordWithDetails, int64, error) {
	skip := (page - 1) * limit

	total, err := r.collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count referral records: %w", err)
	}

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$sort", Value: bson.D{{Key: "created_at", Value: -1}}}},
		bson.D{{Key: "$skip", Value: skip}},
		bson.D{{Key: "$limit", Value: limit}},
		bson.D{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: "users"},
			{Key: "localField", Value: "referrer_id"},
			{Key: "foreignField", Value: "_id"},
			{Key: "as", Value: "referrer"},
		}}},
		bson.D{{Key: "$unwind", Value: bson.D{
			{Key: "path", Value: "$referrer"},
			{Key: "preserveNullAndEmptyArrays", Value: true},
		}}},
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 1},
			{Key: "referrer_id", Value: 1},
			{Key: "referrer_name", Value: "$referrer.name"},
			{Key: "referrer_email", Value: "$referrer.email"},
			{Key: "referred_email", Value: 1},
			{Key: "status", Value: 1},
			{Key: "reward_days_credited", Value: 1},
			{Key: "created_at", Value: 1},
			{Key: "updated_at", Value: 1},
		}}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query referral records: %w", err)
	}
	defer cursor.Close(ctx)

	var records []*domain.ReferralRecordWithDetails
	if err := cursor.All(ctx, &records); err != nil {
		return nil, 0, fmt.Errorf("failed to decode referral records: %w", err)
	}

	if records == nil {
		records = []*domain.ReferralRecordWithDetails{}
	}

	return records, total, nil
}
