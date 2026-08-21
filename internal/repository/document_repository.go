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

type documentRepository struct {
	collection *mongo.Collection
}

// NewDocumentRepository initializes a new DocumentRepository.
func NewDocumentRepository(db *mongo.Database) domain.DocumentRepository {
	col := db.Collection("documents")

	// Ensure compound index on user_id for fast queries per user
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "name", Value: 1},
		},
	})

	return &documentRepository{
		collection: col,
	}
}

// Create inserts a new document record into MongoDB.
func (r *documentRepository) Create(ctx context.Context, doc *domain.Document) (*domain.Document, error) {
	now := time.Now().UTC()
	doc.CreatedAt = now
	doc.UpdatedAt = now

	result, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("failed to insert document: %w", err)
	}

	doc.ID = result.InsertedID.(bson.ObjectID)
	return doc, nil
}

// FindAllByUserID retrieves all documents belonging to a user, with optional search query filter.
func (r *documentRepository) FindAllByUserID(ctx context.Context, userID bson.ObjectID, searchQuery string) ([]*domain.Document, int64, error) {
	filter := bson.M{"user_id": userID}

	if searchQuery != "" {
		filter["$or"] = []bson.M{
			{"name": bson.M{"$regex": searchQuery, "$options": "i"}},
			{"category": bson.M{"$regex": searchQuery, "$options": "i"}},
		}
	}

	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query documents: %w", err)
	}
	defer cursor.Close(ctx)

	var docs []*domain.Document
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, 0, fmt.Errorf("failed to decode documents: %w", err)
	}

	if docs == nil {
		docs = []*domain.Document{}
	}

	total := int64(len(docs))
	return docs, total, nil
}

// FindByID retrieves a single document record by ID.
func (r *documentRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.Document, error) {
	var doc domain.Document
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("document not found")
		}
		return nil, fmt.Errorf("failed to find document: %w", err)
	}
	return &doc, nil
}

// Update modifies an existing document record.
func (r *documentRepository) Update(ctx context.Context, id, userID bson.ObjectID, dto *domain.UpdateDocumentDTO) (*domain.Document, error) {
	updateFields := bson.M{
		"updated_at": time.Now().UTC(),
	}

	if dto.Name != nil {
		updateFields["name"] = *dto.Name
	}
	if dto.Category != nil {
		updateFields["category"] = *dto.Category
	}
	if dto.DocumentURL != nil {
		updateFields["document_url"] = *dto.DocumentURL
	}
	if dto.PublicID != nil {
		updateFields["public_id"] = *dto.PublicID
	}
	if dto.FileType != nil {
		updateFields["file_type"] = *dto.FileType
	}
	if dto.FileSize != nil {
		updateFields["file_size"] = *dto.FileSize
	}

	filter := bson.M{"_id": id, "user_id": userID}
	update := bson.M{"$set": updateFields}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updatedDoc domain.Document
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedDoc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("document not found or access denied")
		}
		return nil, fmt.Errorf("failed to update document: %w", err)
	}

	return &updatedDoc, nil
}

// Delete removes a document record from MongoDB.
func (r *documentRepository) Delete(ctx context.Context, id, userID bson.ObjectID) error {
	filter := bson.M{"_id": id, "user_id": userID}

	result, err := r.collection.DeleteOne(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to delete document: %w", err)
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("document not found or access denied")
	}

	return nil
}

// DeleteAllByUserID removes all document records belonging to a specific user.
func (r *documentRepository) DeleteAllByUserID(ctx context.Context, userID bson.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}
