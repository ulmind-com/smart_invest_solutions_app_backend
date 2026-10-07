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

const announcementsCollection = "announcements"

type announcementRepository struct {
	collection *mongo.Collection
}

// NewAnnouncementRepository initializes a new AnnouncementRepository.
func NewAnnouncementRepository(db *mongo.Database) domain.AnnouncementRepository {
	col := db.Collection(announcementsCollection)

	// Every device asks "what should be on screen right now", so that is the query worth indexing:
	// the active flag and the audience narrow it, and the sort keys avoid an in-memory sort.
	_, _ = col.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "is_active", Value: 1},
			{Key: "audience", Value: 1},
			{Key: "priority", Value: -1},
			{Key: "created_at", Value: -1},
		},
	})

	return &announcementRepository{collection: col}
}

// Create inserts a new announcement.
func (r *announcementRepository) Create(ctx context.Context, announcement *domain.Announcement) (*domain.Announcement, error) {
	now := time.Now().UTC()
	announcement.CreatedAt = now
	announcement.UpdatedAt = now

	result, err := r.collection.InsertOne(ctx, announcement)
	if err != nil {
		return nil, fmt.Errorf("failed to create announcement: %w", err)
	}

	announcement.ID = result.InsertedID.(bson.ObjectID)
	return announcement, nil
}

// FindByID retrieves one announcement.
func (r *announcementRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.Announcement, error) {
	var announcement domain.Announcement
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&announcement)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, fmt.Errorf("announcement not found")
		}
		return nil, fmt.Errorf("failed to find announcement: %w", err)
	}
	return &announcement, nil
}

// FindLive returns what belongs on screen right now for these audiences.
//
// The window is applied here rather than after reading, so a scheduled or expired banner never
// reaches a device: a draft offer is not something to trust a client build to hide.
func (r *announcementRepository) FindLive(ctx context.Context, audiences []string, now time.Time) ([]*domain.Announcement, error) {
	if len(audiences) == 0 {
		return []*domain.Announcement{}, nil
	}

	filter := bson.M{
		"is_active": true,
		"audience":  bson.M{"$in": audiences},
		// A missing or null bound means "unbounded" — matched explicitly, since a document written
		// before the field existed has no key at all.
		"$and": bson.A{
			bson.M{"$or": bson.A{
				bson.M{"starts_at": nil},
				bson.M{"starts_at": bson.M{"$lte": now}},
			}},
			bson.M{"$or": bson.A{
				bson.M{"ends_at": nil},
				bson.M{"ends_at": bson.M{"$gte": now}},
			}},
		},
	}

	opts := options.Find().SetSort(bson.D{
		{Key: "priority", Value: -1},
		{Key: "created_at", Value: -1},
	})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to read announcements: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []*domain.Announcement
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("failed to decode announcements: %w", err)
	}
	if rows == nil {
		rows = []*domain.Announcement{}
	}
	return rows, nil
}

// FindAll returns every announcement, newest first, for the Super Admin's management list.
func (r *announcementRepository) FindAll(ctx context.Context, page, limit int64) ([]*domain.Announcement, int64, error) {
	total, err := r.collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count announcements: %w", err)
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip((page - 1) * limit).
		SetLimit(limit)

	cursor, err := r.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read announcements: %w", err)
	}
	defer cursor.Close(ctx)

	var rows []*domain.Announcement
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, 0, fmt.Errorf("failed to decode announcements: %w", err)
	}
	if rows == nil {
		rows = []*domain.Announcement{}
	}
	return rows, total, nil
}

// Update applies a patch. Only the fields the caller set are written; the Clear* flags are how a
// field gets emptied, since a nil pointer already means "leave it alone".
func (r *announcementRepository) Update(ctx context.Context, id bson.ObjectID, dto *domain.UpdateAnnouncementDTO) (*domain.Announcement, error) {
	set := bson.M{"updated_at": time.Now().UTC()}
	unset := bson.M{}

	if dto.Title != nil {
		set["title"] = *dto.Title
	}
	if dto.Description != nil {
		set["description"] = *dto.Description
	}
	if dto.LinkURL != nil {
		set["link_url"] = *dto.LinkURL
	}
	if dto.CTALabel != nil {
		set["cta_label"] = *dto.CTALabel
	}
	if dto.Audience != nil {
		set["audience"] = *dto.Audience
	}
	if dto.IsActive != nil {
		set["is_active"] = *dto.IsActive
	}
	if dto.Priority != nil {
		set["priority"] = *dto.Priority
	}

	switch {
	case dto.ClearStart:
		unset["starts_at"] = ""
	case dto.StartsAt != nil:
		set["starts_at"] = *dto.StartsAt
	}
	switch {
	case dto.ClearEnd:
		unset["ends_at"] = ""
	case dto.EndsAt != nil:
		set["ends_at"] = *dto.EndsAt
	}

	if dto.ClearMedia {
		unset["media_url"] = ""
		unset["media_public_id"] = ""
		unset["media_type"] = ""
		unset["thumbnail_url"] = ""
	} else {
		if dto.MediaURL != nil {
			set["media_url"] = *dto.MediaURL
		}
		if dto.MediaPublicID != nil {
			set["media_public_id"] = *dto.MediaPublicID
		}
		if dto.MediaType != nil {
			set["media_type"] = *dto.MediaType
		}
		if dto.ThumbnailURL != nil {
			set["thumbnail_url"] = *dto.ThumbnailURL
		} else if dto.MediaURL != nil {
			// A replacement image has no poster frame of its own; leaving the old video's thumbnail
			// behind would show the previous banner's still beside the new picture.
			unset["thumbnail_url"] = ""
		}
	}

	update := bson.M{"$set": set}
	if len(unset) > 0 {
		update["$unset"] = unset
	}

	var saved domain.Announcement
	err := r.collection.FindOneAndUpdate(
		ctx,
		bson.M{"_id": id},
		update,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&saved)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, fmt.Errorf("announcement not found")
		}
		return nil, fmt.Errorf("failed to update announcement: %w", err)
	}

	return &saved, nil
}

// Delete removes an announcement. Purging its media is the service's job, which knows the resource
// type the asset has to be deleted with.
func (r *announcementRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete announcement: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("announcement not found")
	}
	return nil
}
