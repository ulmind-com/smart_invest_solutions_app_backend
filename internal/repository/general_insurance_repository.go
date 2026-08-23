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

type generalInsuranceRepository struct {
	collection *mongo.Collection
}

// NewGeneralInsuranceRepository initializes a new GeneralInsuranceRepository.
func NewGeneralInsuranceRepository(db *mongo.Database) domain.GeneralInsuranceRepository {
	col := db.Collection("general_insurances")

	// Ensure index on user_id for fast policy queries per user
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}},
	})

	return &generalInsuranceRepository{
		collection: col,
	}
}

// Create inserts a new general insurance policy into MongoDB.
func (r *generalInsuranceRepository) Create(ctx context.Context, policy *domain.GeneralInsurance) (*domain.GeneralInsurance, error) {
	now := time.Now().UTC()
	policy.CreatedAt = now
	policy.UpdatedAt = now

	result, err := r.collection.InsertOne(ctx, policy)
	if err != nil {
		return nil, fmt.Errorf("failed to insert general insurance policy: %w", err)
	}

	policy.ID = result.InsertedID.(bson.ObjectID)
	return policy, nil
}

// FindAllByUserID retrieves all general insurance policies belonging to a specific user.
func (r *generalInsuranceRepository) FindAllByUserID(ctx context.Context, userID bson.ObjectID) ([]*domain.GeneralInsurance, int64, error) {
	filter := bson.M{"user_id": userID}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query general insurance policies: %w", err)
	}
	defer cursor.Close(ctx)

	var policies []*domain.GeneralInsurance
	if err := cursor.All(ctx, &policies); err != nil {
		return nil, 0, fmt.Errorf("failed to decode general insurance policies: %w", err)
	}

	if policies == nil {
		policies = []*domain.GeneralInsurance{}
	}

	total := int64(len(policies))
	return policies, total, nil
}

// FindByID retrieves a single general insurance policy by ID.
func (r *generalInsuranceRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.GeneralInsurance, error) {
	var policy domain.GeneralInsurance
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&policy)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("general insurance policy not found")
		}
		return nil, fmt.Errorf("failed to find general insurance policy: %w", err)
	}
	return &policy, nil
}

// Update modifies an existing general insurance policy record belonging to a specific user.
func (r *generalInsuranceRepository) Update(ctx context.Context, id, userID bson.ObjectID, dto *domain.UpdateGeneralInsuranceDTO) (*domain.GeneralInsurance, error) {
	updateFields := bson.M{
		"updated_at": time.Now().UTC(),
	}

	if dto.VehicleNo != nil {
		updateFields["vehicle_no"] = *dto.VehicleNo
	}
	if dto.PolicyNo != nil {
		updateFields["policy_no"] = *dto.PolicyNo
	}
	if dto.DateOfExpiry != nil {
		updateFields["date_of_expiry"] = *dto.DateOfExpiry
	}
	if dto.CompanyName != nil {
		updateFields["company_name"] = *dto.CompanyName
	}
	if dto.AdvisorName != nil {
		updateFields["advisor_name"] = *dto.AdvisorName
	}
	if dto.AdvisorContact != nil {
		updateFields["advisor_contact"] = *dto.AdvisorContact
	}

	filter := bson.M{"_id": id, "user_id": userID}
	update := bson.M{"$set": updateFields}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updatedPolicy domain.GeneralInsurance
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedPolicy)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("general insurance policy not found or access denied")
		}
		return nil, fmt.Errorf("failed to update policy: %w", err)
	}

	return &updatedPolicy, nil
}

// Delete removes a general insurance policy record.
func (r *generalInsuranceRepository) Delete(ctx context.Context, id, userID bson.ObjectID) error {
	filter := bson.M{"_id": id, "user_id": userID}

	result, err := r.collection.DeleteOne(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to delete general insurance policy: %w", err)
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("general insurance policy not found or access denied")
	}

	return nil
}

// DeleteAllByUserID deletes all general insurance policy records belonging to a user.
func (r *generalInsuranceRepository) DeleteAllByUserID(ctx context.Context, userID bson.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}

// FindAllAdmin retrieves a paginated master list of every general insurance policy across all
// clients, each row enriched (via $lookup on the users collection) with the owning customer's
// name and contact number — this is what powers the Admin "who has which policy" dashboard view.
func (r *generalInsuranceRepository) FindAllAdmin(ctx context.Context, page, limit int64) ([]*domain.GeneralInsuranceWithCustomer, int64, error) {
	skip := (page - 1) * limit

	total, err := r.collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count general insurance policies: %w", err)
	}

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$sort", Value: bson.D{{Key: "created_at", Value: -1}}}},
		bson.D{{Key: "$skip", Value: skip}},
		bson.D{{Key: "$limit", Value: limit}},
		bson.D{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: usersCollection},
			{Key: "localField", Value: "user_id"},
			{Key: "foreignField", Value: "_id"},
			{Key: "as", Value: "customer"},
		}}},
		bson.D{{Key: "$unwind", Value: bson.D{
			{Key: "path", Value: "$customer"},
			{Key: "preserveNullAndEmptyArrays", Value: true},
		}}},
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 1},
			{Key: "user_id", Value: 1},
			{Key: "customer_name", Value: "$customer.name"},
			{Key: "contact_no", Value: "$customer.phone"},
			{Key: "vehicle_no", Value: 1},
			{Key: "policy_no", Value: 1},
			{Key: "date_of_expiry", Value: 1},
			{Key: "company_name", Value: 1},
			{Key: "advisor_name", Value: 1},
			{Key: "advisor_contact", Value: 1},
			{Key: "created_at", Value: 1},
			{Key: "updated_at", Value: 1},
		}}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query general insurance policies: %w", err)
	}
	defer cursor.Close(ctx)

	var policies []*domain.GeneralInsuranceWithCustomer
	if err := cursor.All(ctx, &policies); err != nil {
		return nil, 0, fmt.Errorf("failed to decode general insurance policies: %w", err)
	}

	if policies == nil {
		policies = []*domain.GeneralInsuranceWithCustomer{}
	}

	return policies, total, nil
}
