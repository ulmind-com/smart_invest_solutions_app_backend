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

type familyMemberRepository struct {
	collection *mongo.Collection
}

// NewFamilyMemberRepository initializes a new FamilyMemberRepository.
func NewFamilyMemberRepository(db *mongo.Database) domain.FamilyMemberRepository {
	col := db.Collection("family_members")

	// Ensure index on user_id for fast queries by HOF user
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}},
	})

	return &familyMemberRepository{
		collection: col,
	}
}

// Create inserts a new family member document into MongoDB.
func (r *familyMemberRepository) Create(ctx context.Context, member *domain.FamilyMember) (*domain.FamilyMember, error) {
	now := time.Now().UTC()
	member.CreatedAt = now
	member.UpdatedAt = now

	result, err := r.collection.InsertOne(ctx, member)
	if err != nil {
		return nil, fmt.Errorf("failed to insert family member: %w", err)
	}

	member.ID = result.InsertedID.(bson.ObjectID)
	return member, nil
}

// FindAllByUserID retrieves all family members belonging to a specific user.
func (r *familyMemberRepository) FindAllByUserID(ctx context.Context, userID bson.ObjectID) ([]*domain.FamilyMember, int64, error) {
	filter := bson.M{"user_id": userID}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query family members: %w", err)
	}
	defer cursor.Close(ctx)

	var members []*domain.FamilyMember
	if err := cursor.All(ctx, &members); err != nil {
		return nil, 0, fmt.Errorf("failed to decode family members: %w", err)
	}

	if members == nil {
		members = []*domain.FamilyMember{}
	}

	total := int64(len(members))
	return members, total, nil
}

// FindByID retrieves a single family member by ID.
func (r *familyMemberRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.FamilyMember, error) {
	var member domain.FamilyMember
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&member)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("family member not found")
		}
		return nil, fmt.Errorf("failed to find family member: %w", err)
	}
	return &member, nil
}

// Update modifies an existing family member record belonging to a specific user.
func (r *familyMemberRepository) Update(ctx context.Context, id, userID bson.ObjectID, dto *domain.UpdateFamilyMemberDTO) (*domain.FamilyMember, error) {
	updateFields := bson.M{
		"updated_at": time.Now().UTC(),
	}

	if dto.Name != nil {
		updateFields["name"] = *dto.Name
	}
	if dto.RelationWithHOF != nil {
		updateFields["relation_with_hof"] = *dto.RelationWithHOF
	}
	if dto.Phone != nil {
		updateFields["phone"] = *dto.Phone
	}
	if dto.Email != nil {
		updateFields["email"] = *dto.Email
	}
	if dto.BloodGroup != nil {
		updateFields["blood_group"] = *dto.BloodGroup
	}
	if dto.DateOfBirth != nil {
		updateFields["date_of_birth"] = *dto.DateOfBirth
	}

	filter := bson.M{"_id": id, "user_id": userID}
	update := bson.M{"$set": updateFields}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updatedMember domain.FamilyMember
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedMember)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("family member not found or access denied")
		}
		return nil, fmt.Errorf("failed to update family member: %w", err)
	}

	return &updatedMember, nil
}

// Delete removes a family member record belonging to a specific user.
func (r *familyMemberRepository) Delete(ctx context.Context, id, userID bson.ObjectID) error {
	filter := bson.M{"_id": id, "user_id": userID}

	result, err := r.collection.DeleteOne(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to delete family member: %w", err)
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("family member not found or access denied")
	}

	return nil
}

// DeleteAllByUserID deletes all family member records belonging to a user.
func (r *familyMemberRepository) DeleteAllByUserID(ctx context.Context, userID bson.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}
