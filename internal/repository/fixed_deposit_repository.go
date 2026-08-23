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

type fixedDepositRepository struct {
	collection *mongo.Collection
}

// NewFixedDepositRepository initializes a new FixedDepositRepository.
func NewFixedDepositRepository(db *mongo.Database) domain.FixedDepositRepository {
	col := db.Collection("fixed_deposits")

	// Unique index on FD number — prevents the same instrument being recorded twice
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "fd_number", Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	// Ascending index on maturity date — optimizes future maturity-tracking alert queries
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "maturity_date", Value: 1}},
	})

	// Index on user_id for fast per-client queries
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}},
	})

	return &fixedDepositRepository{collection: col}
}

// Create inserts a new Fixed Deposit into MongoDB.
func (r *fixedDepositRepository) Create(ctx context.Context, fd *domain.FixedDeposit) (*domain.FixedDeposit, error) {
	now := time.Now().UTC()
	fd.CreatedAt = now
	fd.UpdatedAt = now

	result, err := r.collection.InsertOne(ctx, fd)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, fmt.Errorf("a fixed deposit with this FD number already exists")
		}
		return nil, fmt.Errorf("failed to insert fixed deposit: %w", err)
	}

	fd.ID = result.InsertedID.(bson.ObjectID)
	return fd, nil
}

// GetByID retrieves a single Fixed Deposit by ID.
func (r *fixedDepositRepository) GetByID(ctx context.Context, id bson.ObjectID) (*domain.FixedDeposit, error) {
	var fd domain.FixedDeposit
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&fd)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("fixed deposit not found")
		}
		return nil, fmt.Errorf("failed to find fixed deposit: %w", err)
	}
	return &fd, nil
}

// GetByUserID retrieves all Fixed Deposits belonging to a specific client.
func (r *fixedDepositRepository) GetByUserID(ctx context.Context, userID bson.ObjectID) ([]*domain.FixedDeposit, int64, error) {
	filter := bson.M{"user_id": userID}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query fixed deposits: %w", err)
	}
	defer cursor.Close(ctx)

	var fds []*domain.FixedDeposit
	if err := cursor.All(ctx, &fds); err != nil {
		return nil, 0, fmt.Errorf("failed to decode fixed deposits: %w", err)
	}

	if fds == nil {
		fds = []*domain.FixedDeposit{}
	}

	return fds, int64(len(fds)), nil
}

// GetAll retrieves a paginated master list of every Fixed Deposit across all clients, optionally
// filtered by is_mapped, each row enriched (via $lookup on the users collection) with the owning
// customer's name and contact number for the Admin dashboard view.
func (r *fixedDepositRepository) GetAll(ctx context.Context, page, limit int64, isMapped *bool) ([]*domain.FixedDepositWithCustomer, int64, error) {
	skip := (page - 1) * limit

	filter := bson.M{}
	if isMapped != nil {
		filter["is_mapped"] = *isMapped
	}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count fixed deposits: %w", err)
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
			{Key: "customer_name", Value: "$customer.name"},
			{Key: "contact_no", Value: "$customer.phone"},
			{Key: "fd_number", Value: 1},
			{Key: "fd_name", Value: 1},
			{Key: "company_name", Value: 1},
			{Key: "principal_amount", Value: 1},
			{Key: "maturity_amount", Value: 1},
			{Key: "term_months", Value: 1},
			{Key: "opening_date", Value: 1},
			{Key: "maturity_date", Value: 1},
			{Key: "nominee_name", Value: 1},
			{Key: "second_holder_name", Value: 1},
			{Key: "account_type", Value: 1},
			{Key: "address", Value: 1},
			{Key: "is_mapped", Value: 1},
			{Key: "created_at", Value: 1},
			{Key: "updated_at", Value: 1},
		}}},
	)

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query fixed deposits: %w", err)
	}
	defer cursor.Close(ctx)

	var fds []*domain.FixedDepositWithCustomer
	if err := cursor.All(ctx, &fds); err != nil {
		return nil, 0, fmt.Errorf("failed to decode fixed deposits: %w", err)
	}

	if fds == nil {
		fds = []*domain.FixedDepositWithCustomer{}
	}

	return fds, total, nil
}

// Update modifies an existing Fixed Deposit. Ownership/RBAC (including the IsMapped
// admin-only-modification rule) is enforced by the service layer before this is called, so the
// filter here is by ID alone, and only non-nil DTO fields are written.
func (r *fixedDepositRepository) Update(ctx context.Context, id bson.ObjectID, dto *domain.UpdateFixedDepositDTO) (*domain.FixedDeposit, error) {
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
	if dto.FDNumber != nil {
		updateFields["fd_number"] = *dto.FDNumber
	}
	if dto.FDName != nil {
		updateFields["fd_name"] = *dto.FDName
	}
	if dto.CompanyName != nil {
		updateFields["company_name"] = *dto.CompanyName
	}
	if dto.PrincipalAmount != nil {
		updateFields["principal_amount"] = *dto.PrincipalAmount
	}
	if dto.MaturityAmount != nil {
		updateFields["maturity_amount"] = *dto.MaturityAmount
	}
	if dto.Term != nil {
		updateFields["term_months"] = *dto.Term
	}
	if dto.OpeningDate != nil {
		updateFields["opening_date"] = *dto.OpeningDate
	}
	if dto.MaturityDate != nil {
		updateFields["maturity_date"] = *dto.MaturityDate
	}
	if dto.NomineeName != nil {
		updateFields["nominee_name"] = *dto.NomineeName
	}
	if dto.SecondHolderName != nil {
		updateFields["second_holder_name"] = *dto.SecondHolderName
	}
	if dto.AccountType != nil {
		updateFields["account_type"] = *dto.AccountType
	}
	if dto.Address != nil {
		updateFields["address"] = *dto.Address
	}
	if dto.IsMapped != nil {
		updateFields["is_mapped"] = *dto.IsMapped
	}

	filter := bson.M{"_id": id}
	update := bson.M{"$set": updateFields}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updatedFD domain.FixedDeposit
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedFD)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("fixed deposit not found")
		}
		if mongo.IsDuplicateKeyError(err) {
			return nil, fmt.Errorf("a fixed deposit with this FD number already exists")
		}
		return nil, fmt.Errorf("failed to update fixed deposit: %w", err)
	}

	return &updatedFD, nil
}

// Delete removes a Fixed Deposit record by ID.
func (r *fixedDepositRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete fixed deposit: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("fixed deposit not found")
	}
	return nil
}

// DeleteAllByUserID deletes all Fixed Deposit records belonging to a user.
func (r *fixedDepositRepository) DeleteAllByUserID(ctx context.Context, userID bson.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}
