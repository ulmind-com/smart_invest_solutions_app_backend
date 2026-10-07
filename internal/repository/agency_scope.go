package repository

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// agencyClientIDs returns the IDs of every account registered under an agency. Repositories whose
// records carry a user_id (but no agency of their own) use it to draw the agency boundary — a
// record belonging to another agency's client must never be read or written across it.
func agencyClientIDs(ctx context.Context, db *mongo.Database, agencyID string) ([]bson.ObjectID, error) {
	cursor, err := db.Collection(usersCollection).Find(ctx,
		bson.M{"agency_id": agencyID},
		options.Find().SetProjection(bson.M{"_id": 1}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve agency clients: %w", err)
	}
	defer cursor.Close(ctx)

	ids := []bson.ObjectID{}
	for cursor.Next(ctx) {
		var doc struct {
			ID bson.ObjectID `bson:"_id"`
		}
		if err := cursor.Decode(&doc); err == nil {
			ids = append(ids, doc.ID)
		}
	}
	return ids, cursor.Err()
}
