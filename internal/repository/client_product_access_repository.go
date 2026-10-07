package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const clientProductAccessCollection = "client_product_access"

type clientProductAccessRepository struct {
	collection *mongo.Collection
}

// NewClientProductAccessRepository initializes a new ClientProductAccessRepository.
func NewClientProductAccessRepository(db *mongo.Database) domain.ClientProductAccessRepository {
	col := db.Collection(clientProductAccessCollection)

	// One override per client, enforced by the database rather than by the upsert alone: two
	// admins saving at once must not be able to leave a client with two conflicting records.
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	return &clientProductAccessRepository{collection: col}
}

// FindByUserID returns the client's override, or nil when they have none.
func (r *clientProductAccessRepository) FindByUserID(ctx context.Context, userID bson.ObjectID) (*domain.ClientProductAccess, error) {
	var access domain.ClientProductAccess
	err := r.collection.FindOne(ctx, bson.M{"user_id": userID}).Decode(&access)
	if err != nil {
		// No record is the normal case — most clients are never restricted — so it is reported as
		// "nothing set", not as a failure.
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read the client's product access: %w", err)
	}
	return &access, nil
}

// Upsert writes the client's override, creating it the first time.
func (r *clientProductAccessRepository) Upsert(ctx context.Context, access *domain.ClientProductAccess) (*domain.ClientProductAccess, error) {
	now := time.Now().UTC()

	productIDs := access.ProductIDs
	if productIDs == nil {
		productIDs = []bson.ObjectID{}
	}

	update := bson.M{
		"$set": bson.M{
			"mode":            access.Mode,
			"product_ids":     productIDs,
			"updated_by_id":   access.UpdatedByID,
			"updated_by_name": access.UpdatedByName,
			"updated_at":      now,
		},
		"$setOnInsert": bson.M{
			"user_id":    access.UserID,
			"created_at": now,
		},
	}

	var saved domain.ClientProductAccess
	err := r.collection.FindOneAndUpdate(
		ctx,
		bson.M{"user_id": access.UserID},
		update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&saved)
	if err != nil {
		return nil, fmt.Errorf("failed to save the client's product access: %w", err)
	}

	return &saved, nil
}

// DeleteByUserID removes a client's override.
func (r *clientProductAccessRepository) DeleteByUserID(ctx context.Context, userID bson.ObjectID) error {
	if _, err := r.collection.DeleteOne(ctx, bson.M{"user_id": userID}); err != nil {
		return fmt.Errorf("failed to delete the client's product access: %w", err)
	}
	return nil
}

// RemoveProductFromAll drops a deleted product from every client's list.
//
// Reads tolerate a stale ID on their own (a product that no longer exists simply doesn't come
// back), but the admin screen counts what is selected — leaving the ID behind would report a
// client as seeing more products than they can.
func (r *clientProductAccessRepository) RemoveProductFromAll(ctx context.Context, productID bson.ObjectID) error {
	_, err := r.collection.UpdateMany(
		ctx,
		bson.M{"product_ids": productID},
		bson.M{"$pull": bson.M{"product_ids": productID}, "$set": bson.M{"updated_at": time.Now().UTC()}},
	)
	if err != nil {
		return fmt.Errorf("failed to drop the deleted product from client access lists: %w", err)
	}
	return nil
}
