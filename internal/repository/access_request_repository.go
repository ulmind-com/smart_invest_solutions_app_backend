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

const accessRequestsCollection = "access_requests"

type accessRequestRepository struct {
	collection *mongo.Collection
}

// NewAccessRequestRepository creates a new MongoDB-backed AccessRequest repository.
func NewAccessRequestRepository(db *mongo.Database) domain.AccessRequestRepository {
	return &accessRequestRepository{
		collection: db.Collection(accessRequestsCollection),
	}
}

// Create inserts a new AccessRequest into MongoDB.
func (r *accessRequestRepository) Create(ctx context.Context, req *domain.AccessRequest) (*domain.AccessRequest, error) {
	now := time.Now().UTC()
	req.CreatedAt = now
	req.UpdatedAt = now
	if req.Status == "" {
		req.Status = domain.AccessStatusPending
	}

	result, err := r.collection.InsertOne(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to submit access request: %w", err)
	}

	req.ID = result.InsertedID.(bson.ObjectID)
	return req, nil
}

// FindByID retrieves an AccessRequest by ObjectID.
func (r *accessRequestRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.AccessRequest, error) {
	var req domain.AccessRequest
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&req)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("access request not found")
		}
		return nil, fmt.Errorf("failed to find access request: %w", err)
	}
	return &req, nil
}

// FindByEmail retrieves the most recent AccessRequest by email.
func (r *accessRequestRepository) FindByEmail(ctx context.Context, email string) (*domain.AccessRequest, error) {
	var req domain.AccessRequest
	opts := options.FindOne().SetSort(bson.D{{Key: "created_at", Value: -1}})
	err := r.collection.FindOne(ctx, utils.EmailFilter(email), opts).Decode(&req)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // Return nil when not found without error
		}
		return nil, fmt.Errorf("failed to find access request by email: %w", err)
	}
	return &req, nil
}

// FindAll retrieves paginated AccessRequests, optionally filtered by status and/or agency.
func (r *accessRequestRepository) FindAll(ctx context.Context, status, agencyID string, page, limit int64) ([]*domain.AccessRequest, int64, error) {
	skip := (page - 1) * limit

	filter := bson.M{}
	if status != "" {
		filter["status"] = status
	}
	if agencyID != "" {
		filter["applied_agency_id"] = agencyID
	}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count access requests: %w", err)
	}

	opts := options.Find().
		SetSkip(skip).
		SetLimit(limit).
		SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list access requests: %w", err)
	}
	defer cursor.Close(ctx)

	var requests []*domain.AccessRequest
	if err := cursor.All(ctx, &requests); err != nil {
		return nil, 0, fmt.Errorf("failed to decode access requests: %w", err)
	}

	return requests, total, nil
}

// UpdateStatus updates the status and admin notes for an AccessRequest.
func (r *accessRequestRepository) UpdateStatus(ctx context.Context, id bson.ObjectID, status string, adminNotes string) (*domain.AccessRequest, error) {
	update := bson.M{
		"$set": bson.M{
			"status":      status,
			"admin_notes": adminNotes,
			"updated_at":  time.Now().UTC(),
		},
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var req domain.AccessRequest
	err := r.collection.FindOneAndUpdate(ctx, bson.M{"_id": id}, update, opts).Decode(&req)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("access request not found")
		}
		return nil, fmt.Errorf("failed to update access request status: %w", err)
	}

	return &req, nil
}

// UpdateDetailsAndStatus updates the details and resets status of an existing AccessRequest.
func (r *accessRequestRepository) UpdateDetailsAndStatus(ctx context.Context, id bson.ObjectID, name, phone, notes, appliedReferralCode, appliedAgencyID, status string) (*domain.AccessRequest, error) {
	update := bson.M{
		"$set": bson.M{
			"name":                  name,
			"phone":                 phone,
			"notes":                 notes,
			"applied_referral_code": appliedReferralCode,
			"applied_agency_id":     appliedAgencyID,
			"status":                status,
			"admin_notes":           "",
			"updated_at":            time.Now().UTC(),
		},
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var req domain.AccessRequest
	err := r.collection.FindOneAndUpdate(ctx, bson.M{"_id": id}, update, opts).Decode(&req)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("access request not found")
		}
		return nil, fmt.Errorf("failed to update access request details: %w", err)
	}

	return &req, nil
}
