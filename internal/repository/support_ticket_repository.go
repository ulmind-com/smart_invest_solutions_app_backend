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

type supportTicketRepository struct {
	collection *mongo.Collection
}

// NewSupportTicketRepository initializes a new SupportTicketRepository.
func NewSupportTicketRepository(db *mongo.Database) domain.SupportTicketRepository {
	col := db.Collection("support_tickets")

	// Unique index on ticket number — prevents two tickets ever sharing the same human-facing ID
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "ticket_number", Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	// Index on status — optimizes admin workflow queries (e.g. filtering Open/In_Progress tickets)
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "status", Value: 1}},
	})

	// Index on user_id for fast per-client queries
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}},
	})

	return &supportTicketRepository{collection: col}
}

// Create inserts a new support ticket into MongoDB.
func (r *supportTicketRepository) Create(ctx context.Context, ticket *domain.SupportTicket) (*domain.SupportTicket, error) {
	now := time.Now().UTC()
	ticket.CreatedAt = now
	ticket.UpdatedAt = now

	result, err := r.collection.InsertOne(ctx, ticket)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, fmt.Errorf("ticket number already exists")
		}
		return nil, fmt.Errorf("failed to insert support ticket: %w", err)
	}

	ticket.ID = result.InsertedID.(bson.ObjectID)
	return ticket, nil
}

// GetByID retrieves a single support ticket by ID.
func (r *supportTicketRepository) GetByID(ctx context.Context, id bson.ObjectID) (*domain.SupportTicket, error) {
	var ticket domain.SupportTicket
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&ticket)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("support ticket not found")
		}
		return nil, fmt.Errorf("failed to find support ticket: %w", err)
	}
	return &ticket, nil
}

// GetByUserID retrieves all support tickets belonging to a specific client, optionally filtered
// by status and/or category.
func (r *supportTicketRepository) GetByUserID(ctx context.Context, userID bson.ObjectID, status, category string) ([]*domain.SupportTicket, int64, error) {
	filter := bson.M{"user_id": userID}
	if status != "" {
		filter["status"] = status
	}
	if category != "" {
		filter["category"] = category
	}

	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query support tickets: %w", err)
	}
	defer cursor.Close(ctx)

	var tickets []*domain.SupportTicket
	if err := cursor.All(ctx, &tickets); err != nil {
		return nil, 0, fmt.Errorf("failed to decode support tickets: %w", err)
	}

	if tickets == nil {
		tickets = []*domain.SupportTicket{}
	}

	return tickets, int64(len(tickets)), nil
}

// GetAll retrieves a paginated master list of every support ticket across all clients, optionally
// filtered by status, category, and/or the raising client's Agency ID, each row enriched (via
// $lookup on the users collection) with the requesting customer's name, contact number, and Agency
// ID. The agency filter runs against the joined customer document (agency_id lives on the user, not
// the ticket), so filtering and enrichment share one pipeline.
func (r *supportTicketRepository) GetAll(ctx context.Context, page, limit int64, status, category, agencyID string) ([]*domain.SupportTicketWithCustomer, int64, error) {
	skip := (page - 1) * limit

	basePipeline := mongo.Pipeline{}
	matchStage := bson.D{}
	if status != "" {
		matchStage = append(matchStage, bson.E{Key: "status", Value: status})
	}
	if category != "" {
		matchStage = append(matchStage, bson.E{Key: "category", Value: category})
	}
	if len(matchStage) > 0 {
		basePipeline = append(basePipeline, bson.D{{Key: "$match", Value: matchStage}})
	}
	basePipeline = append(basePipeline,
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
	)
	if agencyID != "" {
		basePipeline = append(basePipeline, bson.D{{Key: "$match", Value: bson.D{{Key: "customer.agency_id", Value: agencyID}}}})
	}

	countPipeline := append(mongo.Pipeline{}, basePipeline...)
	countPipeline = append(countPipeline, bson.D{{Key: "$count", Value: "total"}})

	countCursor, err := r.collection.Aggregate(ctx, countPipeline)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count support tickets: %w", err)
	}
	defer countCursor.Close(ctx)

	var countResult []struct {
		Total int64 `bson:"total"`
	}
	if err := countCursor.All(ctx, &countResult); err != nil {
		return nil, 0, fmt.Errorf("failed to decode support ticket count: %w", err)
	}
	var total int64
	if len(countResult) > 0 {
		total = countResult[0].Total
	}

	pipeline := append(mongo.Pipeline{}, basePipeline...)
	pipeline = append(pipeline,
		bson.D{{Key: "$sort", Value: bson.D{{Key: "created_at", Value: -1}}}},
		bson.D{{Key: "$skip", Value: skip}},
		bson.D{{Key: "$limit", Value: limit}},
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 1},
			{Key: "user_id", Value: 1},
			{Key: "customer_name", Value: "$customer.name"},
			{Key: "contact_no", Value: "$customer.phone"},
			{Key: "agency_id", Value: "$customer.agency_id"},
			{Key: "ticket_number", Value: 1},
			{Key: "category", Value: 1},
			{Key: "subject", Value: 1},
			{Key: "description", Value: 1},
			{Key: "status", Value: 1},
			{Key: "admin_notes", Value: 1},
			{Key: "created_at", Value: 1},
			{Key: "updated_at", Value: 1},
		}}},
	)

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query support tickets: %w", err)
	}
	defer cursor.Close(ctx)

	var tickets []*domain.SupportTicketWithCustomer
	if err := cursor.All(ctx, &tickets); err != nil {
		return nil, 0, fmt.Errorf("failed to decode support tickets: %w", err)
	}

	if tickets == nil {
		tickets = []*domain.SupportTicketWithCustomer{}
	}

	return tickets, total, nil
}

// Update modifies an existing support ticket. Ownership/RBAC (including the Status/AdminNotes
// client-restriction rules) is enforced by the service layer before this is called, so the filter
// here is by ID alone, and only non-nil DTO fields are written.
func (r *supportTicketRepository) Update(ctx context.Context, id bson.ObjectID, dto *domain.UpdateSupportTicketDTO) (*domain.SupportTicket, error) {
	updateFields := bson.M{
		"updated_at": time.Now().UTC(),
	}

	if dto.Subject != nil {
		updateFields["subject"] = *dto.Subject
	}
	if dto.Description != nil {
		updateFields["description"] = *dto.Description
	}
	if dto.Status != nil {
		updateFields["status"] = *dto.Status
	}
	if dto.AdminNotes != nil {
		updateFields["admin_notes"] = *dto.AdminNotes
	}

	filter := bson.M{"_id": id}
	update := bson.M{"$set": updateFields}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var updatedTicket domain.SupportTicket
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedTicket)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("support ticket not found")
		}
		return nil, fmt.Errorf("failed to update support ticket: %w", err)
	}

	return &updatedTicket, nil
}

// Delete removes a support ticket record by ID.
func (r *supportTicketRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete support ticket: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("support ticket not found")
	}
	return nil
}

// DeleteAllByUserID deletes all support ticket records belonging to a user.
func (r *supportTicketRepository) DeleteAllByUserID(ctx context.Context, userID bson.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}
