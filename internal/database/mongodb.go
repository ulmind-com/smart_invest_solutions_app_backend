package database

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoDB holds the database client and database reference.
type MongoDB struct {
	Client   *mongo.Client
	Database *mongo.Database
}

// Connect establishes a connection to MongoDB Atlas and verifies it with a ping.
func Connect(uri, dbName string) (*MongoDB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Set client options
	clientOptions := options.Client().ApplyURI(uri)

	// Connect to MongoDB
	client, err := mongo.Connect(clientOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Ping the database to verify connection
	if err := client.Database(dbName).RunCommand(ctx, bson.D{{Key: "ping", Value: 1}}).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	log.Info().Str("database", dbName).Msg("Successfully connected to MongoDB Atlas")

	return &MongoDB{
		Client:   client,
		Database: client.Database(dbName),
	}, nil
}

// GetCollection returns a handle to a specific collection in the database.
func (m *MongoDB) GetCollection(name string) *mongo.Collection {
	return m.Database.Collection(name)
}

// Disconnect gracefully closes the MongoDB connection.
func (m *MongoDB) Disconnect(ctx context.Context) error {
	if err := m.Client.Disconnect(ctx); err != nil {
		return fmt.Errorf("failed to disconnect from MongoDB: %w", err)
	}
	log.Info().Msg("Disconnected from MongoDB")
	return nil
}

// HealthCheck verifies the database connection is alive.
func (m *MongoDB) HealthCheck(ctx context.Context) error {
	return m.Client.Database("admin").RunCommand(ctx, bson.D{{Key: "ping", Value: 1}}).Err()
}
