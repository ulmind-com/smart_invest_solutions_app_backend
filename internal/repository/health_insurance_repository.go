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

type healthInsuranceRepository struct {
	collection *mongo.Collection
}

// NewHealthInsuranceRepository initializes a new HealthInsuranceRepository.
func NewHealthInsuranceRepository(db *mongo.Database) domain.HealthInsuranceRepository {
	col := db.Collection("health_insurances")

	// Unique index on policy number — prevents the same policy being recorded twice
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "policy_details.policy_no", Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	// Ascending index on next due date — optimizes future premium/renewal reminder queries
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "premium_details.next_due_date", Value: 1}},
	})

	// Index on user_id for fast per-client queries
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}},
	})

	return &healthInsuranceRepository{collection: col}
}

// Create inserts a new health insurance policy into MongoDB.
func (r *healthInsuranceRepository) Create(ctx context.Context, policy *domain.HealthInsurance) (*domain.HealthInsurance, error) {
	now := time.Now().UTC()
	policy.CreatedAt = now
	policy.UpdatedAt = now

	result, err := r.collection.InsertOne(ctx, policy)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, fmt.Errorf("policy number already exists")
		}
		return nil, fmt.Errorf("failed to insert health insurance policy: %w", err)
	}

	policy.ID = result.InsertedID.(bson.ObjectID)
	return policy, nil
}

// GetByID retrieves a single health insurance policy by ID.
func (r *healthInsuranceRepository) GetByID(ctx context.Context, id bson.ObjectID) (*domain.HealthInsurance, error) {
	var policy domain.HealthInsurance
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&policy)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("health insurance policy not found")
		}
		return nil, fmt.Errorf("failed to find health insurance policy: %w", err)
	}
	return &policy, nil
}

// GetByUserID retrieves all health insurance policies belonging to a specific client.
func (r *healthInsuranceRepository) GetByUserID(ctx context.Context, userID bson.ObjectID) ([]*domain.HealthInsurance, int64, error) {
	filter := bson.M{"user_id": userID}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query health insurance policies: %w", err)
	}
	defer cursor.Close(ctx)

	var policies []*domain.HealthInsurance
	if err := cursor.All(ctx, &policies); err != nil {
		return nil, 0, fmt.Errorf("failed to decode health insurance policies: %w", err)
	}

	if policies == nil {
		policies = []*domain.HealthInsurance{}
	}

	return policies, int64(len(policies)), nil
}

// GetAll retrieves a paginated master list of every health insurance policy across all clients,
// optionally filtered by is_mapped, each row enriched (via $lookup on the users collection) with
// the owning customer's name and contact number for the Admin dashboard view.
func (r *healthInsuranceRepository) GetAll(ctx context.Context, page, limit int64, isMapped *bool) ([]*domain.HealthInsuranceWithCustomer, int64, error) {
	skip := (page - 1) * limit

	filter := bson.M{}
	if isMapped != nil {
		filter["is_mapped"] = *isMapped
	}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count health insurance policies: %w", err)
	}

	pipeline := mongo.Pipeline{}
	if isMapped != nil {
		pipeline = append(pipeline, bson.D{{Key: "$match", Value: bson.D{{Key: "is_mapped", Value: *isMapped}}}})
	}
	pipeline = append(pipeline,
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
			{Key: "family_member_id", Value: 1},
			{Key: "company_name", Value: 1},
			{Key: "customer_name", Value: "$customer.name"},
			{Key: "contact_no", Value: "$customer.phone"},
			{Key: "policy_details", Value: 1},
			{Key: "premium_details", Value: 1},
			{Key: "is_mapped", Value: 1},
			{Key: "created_at", Value: 1},
			{Key: "updated_at", Value: 1},
		}}},
	)

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query health insurance policies: %w", err)
	}
	defer cursor.Close(ctx)

	var policies []*domain.HealthInsuranceWithCustomer
	if err := cursor.All(ctx, &policies); err != nil {
		return nil, 0, fmt.Errorf("failed to decode health insurance policies: %w", err)
	}

	if policies == nil {
		policies = []*domain.HealthInsuranceWithCustomer{}
	}

	return policies, total, nil
}

// Update modifies an existing health insurance policy. Ownership/RBAC (including the IsMapped
// admin-only-modification rule) is enforced by the service layer before this is called, so the
// filter here is by ID alone, and only non-nil DTO fields are written.
func (r *healthInsuranceRepository) Update(ctx context.Context, id bson.ObjectID, dto *domain.UpdateHealthInsuranceDTO) (*domain.HealthInsurance, error) {
	updateFields := bson.M{
		"updated_at": time.Now().UTC(),
	}

	if dto.FamilyMemberID != nil {
		familyMemberID, err := bson.ObjectIDFromHex(*dto.FamilyMemberID)
		if err != nil {
			return nil, fmt.Errorf("invalid family member ID format: %w", err)
		}
		updateFields["family_member_id"] = familyMemberID
	}
	if dto.CompanyName != nil {
		updateFields["company_name"] = *dto.CompanyName
	}
	if dto.PolicyNo != nil {
		updateFields["policy_details.policy_no"] = *dto.PolicyNo
	}
	if dto.PlanName != nil {
		updateFields["policy_details.plan_name"] = *dto.PlanName
	}
	if dto.PrimaryInsuredName != nil {
		updateFields["policy_details.primary_insured_name"] = *dto.PrimaryInsuredName
	}
	if dto.SumInsured != nil {
		updateFields["policy_details.sum_insured"] = *dto.SumInsured
	}
	if dto.DOC != nil {
		updateFields["policy_details.doc"] = *dto.DOC
	}
	if dto.ExpiryDate != nil {
		updateFields["policy_details.expiry_date"] = *dto.ExpiryDate
	}
	if dto.InstallmentPremium != nil {
		updateFields["premium_details.installment_premium"] = *dto.InstallmentPremium
	}
	if dto.NextDueDate != nil {
		updateFields["premium_details.next_due_date"] = *dto.NextDueDate
	}
	if dto.PaymentMode != nil {
		updateFields["premium_details.payment_mode"] = *dto.PaymentMode
	}
	if dto.IsMapped != nil {
		updateFields["is_mapped"] = *dto.IsMapped
	}

	filter := bson.M{"_id": id}
	update := bson.M{"$set": updateFields}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updatedPolicy domain.HealthInsurance
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedPolicy)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("health insurance policy not found")
		}
		if mongo.IsDuplicateKeyError(err) {
			return nil, fmt.Errorf("policy number already exists")
		}
		return nil, fmt.Errorf("failed to update health insurance policy: %w", err)
	}

	return &updatedPolicy, nil
}

// Delete removes a health insurance policy record by ID.
func (r *healthInsuranceRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete health insurance policy: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("health insurance policy not found")
	}
	return nil
}

// DeleteAllByUserID deletes all health insurance policy records belonging to a user.
func (r *healthInsuranceRepository) DeleteAllByUserID(ctx context.Context, userID bson.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}
