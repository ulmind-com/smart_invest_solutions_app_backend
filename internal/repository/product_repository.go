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

type productRepository struct {
	collection *mongo.Collection
}

// NewProductRepository initializes a new ProductRepository.
func NewProductRepository(db *mongo.Database) domain.ProductRepository {
	col := db.Collection("products")

	// Index on category — optimizes client browsing/filtering by product type
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "category", Value: 1}},
	})

	// Index on is_active — optimizes the client-facing "published only" catalog view
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "is_active", Value: 1}},
	})

	return &productRepository{collection: col}
}

// Create inserts a new product into MongoDB.
func (r *productRepository) Create(ctx context.Context, product *domain.Product) (*domain.Product, error) {
	now := time.Now().UTC()
	product.CreatedAt = now
	product.UpdatedAt = now

	result, err := r.collection.InsertOne(ctx, product)
	if err != nil {
		return nil, fmt.Errorf("failed to insert product: %w", err)
	}

	product.ID = result.InsertedID.(bson.ObjectID)
	return product, nil
}

// FindByID retrieves a single product by ID.
func (r *productRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.Product, error) {
	var product domain.Product
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&product)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("product not found")
		}
		return nil, fmt.Errorf("failed to find product: %w", err)
	}
	return &product, nil
}

// FindAll retrieves a paginated list of products, optionally filtered by category and/or
// is_active. isActive == nil returns products regardless of active status (used by admin);
// callers restricting to the public catalog (clients) must pass a non-nil true.
func (r *productRepository) FindAll(ctx context.Context, page, limit int64, category string, isActive *bool) ([]*domain.Product, int64, error) {
	skip := (page - 1) * limit

	filter := bson.M{}
	if category != "" {
		filter["category"] = category
	}
	if isActive != nil {
		filter["is_active"] = *isActive
	}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count products: %w", err)
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip(skip).
		SetLimit(limit)

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query products: %w", err)
	}
	defer cursor.Close(ctx)

	var products []*domain.Product
	if err := cursor.All(ctx, &products); err != nil {
		return nil, 0, fmt.Errorf("failed to decode products: %w", err)
	}

	if products == nil {
		products = []*domain.Product{}
	}

	return products, total, nil
}

// Update modifies an existing product. RBAC is enforced by the service layer before this is
// called, so only non-nil DTO fields are written.
func (r *productRepository) Update(ctx context.Context, id bson.ObjectID, dto *domain.UpdateProductDTO) (*domain.Product, error) {
	updateFields := bson.M{
		"updated_at": time.Now().UTC(),
	}

	if dto.Name != nil {
		updateFields["name"] = *dto.Name
	}
	if dto.Category != nil {
		updateFields["category"] = *dto.Category
	}
	if dto.Description != nil {
		updateFields["description"] = *dto.Description
	}
	if dto.KeyBenefits != nil {
		updateFields["key_benefits"] = *dto.KeyBenefits
	}
	if dto.IsActive != nil {
		updateFields["is_active"] = *dto.IsActive
	}
	if dto.BrochureURL != nil {
		updateFields["brochure_url"] = *dto.BrochureURL
	}
	if dto.BrochurePublicID != nil {
		updateFields["brochure_public_id"] = *dto.BrochurePublicID
	}

	filter := bson.M{"_id": id}
	update := bson.M{"$set": updateFields}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updatedProduct domain.Product
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedProduct)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("product not found")
		}
		return nil, fmt.Errorf("failed to update product: %w", err)
	}

	return &updatedProduct, nil
}

// Delete removes a product record by ID.
func (r *productRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete product: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("product not found")
	}
	return nil
}
