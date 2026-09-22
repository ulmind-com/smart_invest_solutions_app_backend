package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/utils"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const referralRecordsCollection = "referral_records"

type referralRepository struct {
	collection *mongo.Collection
}

// NewReferralRepository initializes a new ReferralRepository.
func NewReferralRepository(db *mongo.Database) domain.ReferralRepository {
	col := db.Collection(referralRecordsCollection)

	// Powers both the per-referrer counts and the "my referrals" ledger.
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "referrer_id", Value: 1},
			{Key: "status", Value: 1},
		},
	})

	// Completing a referral looks it up by the applicant's email.
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "referred_email", Value: 1}},
	})

	return &referralRepository{collection: col}
}

// Create inserts a new referral record, normalising the referred email it is keyed by.
func (r *referralRepository) Create(ctx context.Context, record *domain.ReferralRecord) (*domain.ReferralRecord, error) {
	now := time.Now().UTC()
	record.CreatedAt = now
	record.UpdatedAt = now
	record.ReferredEmail = utils.NormalizeEmail(record.ReferredEmail)
	if record.Status == "" {
		record.Status = domain.ReferralStatusPending
	}

	result, err := r.collection.InsertOne(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("failed to create referral record: %w", err)
	}

	record.ID = result.InsertedID.(bson.ObjectID)
	return record, nil
}

// GetPendingByReferredEmail finds the outstanding referral for an applicant, matching the email
// case-insensitively (records written before emails were normalised may carry other casing).
func (r *referralRepository) GetPendingByReferredEmail(ctx context.Context, email string) (*domain.ReferralRecord, error) {
	filter := bson.M{
		"referred_email": utils.EmailFilter(email)["email"],
		"status":         domain.ReferralStatusPending,
	}

	var record domain.ReferralRecord
	err := r.collection.FindOne(ctx, filter, options.FindOne().SetSort(bson.D{{Key: "created_at", Value: 1}})).Decode(&record)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // No pending referral found
		}
		return nil, fmt.Errorf("failed to query pending referral record: %w", err)
	}

	return &record, nil
}

// Complete marks a pending referral as converted. The status filter makes it idempotent: a second
// approval of the same applicant can't double-count the referral.
func (r *referralRepository) Complete(ctx context.Context, id, referredUserID bson.ObjectID, referredName, referredPhone string) error {
	now := time.Now().UTC()
	set := bson.M{
		"status":           domain.ReferralStatusCompleted,
		"referred_user_id": referredUserID,
		"completed_at":     now,
		"updated_at":       now,
	}
	if referredName != "" {
		set["referred_name"] = referredName
	}
	if referredPhone != "" {
		set["referred_phone"] = referredPhone
	}

	result, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": id, "status": domain.ReferralStatusPending},
		bson.M{"$set": set},
	)
	if err != nil {
		return fmt.Errorf("failed to complete referral record: %w", err)
	}
	if result.MatchedCount == 0 {
		return nil // Already completed by an earlier call — nothing to do.
	}
	return nil
}

// CountsByReferrerID returns one referrer's pending/completed split.
func (r *referralRepository) CountsByReferrerID(ctx context.Context, referrerID bson.ObjectID) (domain.ReferralCounts, error) {
	var counts domain.ReferralCounts

	pending, err := r.collection.CountDocuments(ctx, bson.M{"referrer_id": referrerID, "status": domain.ReferralStatusPending})
	if err != nil {
		return counts, fmt.Errorf("failed to count pending referrals: %w", err)
	}
	completed, err := r.collection.CountDocuments(ctx, bson.M{"referrer_id": referrerID, "status": domain.ReferralStatusCompleted})
	if err != nil {
		return counts, fmt.Errorf("failed to count completed referrals: %w", err)
	}

	counts.Pending, counts.Completed = pending, completed
	return counts, nil
}

// CountsByReferrer groups the whole ledger by referrer in a single aggregation, so the super
// admin's leaderboard costs one query rather than two per admin.
func (r *referralRepository) CountsByReferrer(ctx context.Context) (map[bson.ObjectID]domain.ReferralCounts, error) {
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{{Key: "referrer", Value: "$referrer_id"}, {Key: "status", Value: "$status"}}},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("failed to group referrals by referrer: %w", err)
	}
	defer cursor.Close(ctx)

	counts := map[bson.ObjectID]domain.ReferralCounts{}
	for cursor.Next(ctx) {
		var row struct {
			ID struct {
				Referrer bson.ObjectID `bson:"referrer"`
				Status   string        `bson:"status"`
			} `bson:"_id"`
			Count int64 `bson:"count"`
		}
		if err := cursor.Decode(&row); err != nil {
			continue
		}
		entry := counts[row.ID.Referrer]
		if row.ID.Status == domain.ReferralStatusCompleted {
			entry.Completed += row.Count
		} else {
			entry.Pending += row.Count
		}
		counts[row.ID.Referrer] = entry
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("failed to read referral counts: %w", err)
	}

	return counts, nil
}

// GetAll lists the ledger newest-first, optionally for a single referrer.
func (r *referralRepository) GetAll(ctx context.Context, page, limit int64, referrerID *bson.ObjectID) ([]*domain.ReferralRecordWithDetails, int64, error) {
	skip := (page - 1) * limit

	filter := bson.M{}
	if referrerID != nil {
		filter["referrer_id"] = *referrerID
	}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count referral records: %w", err)
	}

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: filter}},
		bson.D{{Key: "$sort", Value: bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}}},
		bson.D{{Key: "$skip", Value: skip}},
		bson.D{{Key: "$limit", Value: limit}},
		bson.D{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: usersCollection},
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
			{Key: "referrer_admin_id", Value: "$referrer.admin_id"},
			{Key: "referrer_role", Value: "$referrer.role"},
			{Key: "referred_email", Value: 1},
			{Key: "referred_name", Value: 1},
			{Key: "referred_phone", Value: 1},
			{Key: "referred_user_id", Value: 1},
			{Key: "status", Value: 1},
			{Key: "completed_at", Value: 1},
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

// ReassignReferrer moves every referral made by fromUserID to toUserID.
func (r *referralRepository) ReassignReferrer(ctx context.Context, fromUserID, toUserID bson.ObjectID) (int64, error) {
	result, err := r.collection.UpdateMany(ctx,
		bson.M{"referrer_id": fromUserID},
		bson.M{"$set": bson.M{"referrer_id": toUserID, "updated_at": time.Now().UTC()}},
	)
	if err != nil {
		return 0, fmt.Errorf("failed to reassign referral records: %w", err)
	}
	return result.ModifiedCount, nil
}
