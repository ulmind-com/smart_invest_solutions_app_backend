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

const usersCollection = "users"

// userRepository implements domain.UserRepository using MongoDB.
type userRepository struct {
	collection *mongo.Collection
}

// NewUserRepository creates a new MongoDB-backed user repository.
func NewUserRepository(db *mongo.Database) domain.UserRepository {
	return &userRepository{
		collection: db.Collection(usersCollection),
	}
}

// Create inserts a new user document into the database.
// IsActive is intentionally NOT overridden here — the service layer decides the initial value
// (false for public client self-registration pending Admin approval, true for Admin-created accounts).
func (r *userRepository) Create(ctx context.Context, user *domain.User) (*domain.User, error) {
	now := time.Now().UTC()
	user.CreatedAt = now
	user.UpdatedAt = now
	if user.Role == "" {
		user.Role = domain.RoleClient
	}

	result, err := r.collection.InsertOne(ctx, user)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, fmt.Errorf("user with this email already exists")
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	user.ID = result.InsertedID.(bson.ObjectID)
	return user, nil
}

// FindByID retrieves a user by their ObjectID.
func (r *userRepository) FindByID(ctx context.Context, id bson.ObjectID) (*domain.User, error) {
	var user domain.User
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to find user: %w", err)
	}
	return &user, nil
}

// FindByEmail retrieves a user by their email address.
func (r *userRepository) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	var user domain.User
	err := r.collection.FindOne(ctx, bson.M{"email": email}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to find user by email: %w", err)
	}
	return &user, nil
}

// FindByAdminID retrieves a user matching the given admin_id.
func (r *userRepository) FindByAdminID(ctx context.Context, adminID string) (*domain.User, error) {
	var user domain.User
	err := r.collection.FindOne(ctx, bson.M{"admin_id": adminID}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to find user: %w", err)
	}
	return &user, nil
}

// FindAllByRoles retrieves a paginated list of users whose role is in the given set.
func (r *userRepository) FindAllByRoles(ctx context.Context, roles []string, page, limit int64) ([]*domain.User, int64, error) {
	skip := (page - 1) * limit
	filter := bson.M{"role": bson.M{"$in": roles}}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}

	opts := options.Find().
		SetSkip(skip).
		SetLimit(limit).
		SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to find users: %w", err)
	}
	defer cursor.Close(ctx)

	var users []*domain.User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, 0, fmt.Errorf("failed to decode users: %w", err)
	}

	return users, total, nil
}

// FindAll retrieves a paginated list of users.
func (r *userRepository) FindAll(ctx context.Context, page, limit int64) ([]*domain.User, int64, error) {
	skip := (page - 1) * limit

	// Get total count
	total, err := r.collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}

	// Set find options with pagination and sorting
	opts := options.Find().
		SetSkip(skip).
		SetLimit(limit).
		SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := r.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to find users: %w", err)
	}
	defer cursor.Close(ctx)

	var users []*domain.User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, 0, fmt.Errorf("failed to decode users: %w", err)
	}

	return users, total, nil
}

// Update modifies an existing user document.
func (r *userRepository) Update(ctx context.Context, id bson.ObjectID, req *domain.UpdateUserRequest) (*domain.User, error) {
	updateFields := bson.M{
		"updated_at": time.Now().UTC(),
	}

	if req.Name != nil {
		updateFields["name"] = *req.Name
	}
	if req.Email != nil {
		updateFields["email"] = *req.Email
	}
	if req.Phone != nil {
		updateFields["phone"] = *req.Phone
	}
	if req.Role != nil {
		updateFields["role"] = *req.Role
	}
	if req.IsActive != nil {
		updateFields["is_active"] = *req.IsActive
	}

	filter := bson.M{"_id": id}
	update := bson.M{"$set": updateFields}
	opts := options.FindOneAndUpdate().
		SetReturnDocument(options.After)

	var user domain.User
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to update user: %w", err)
	}

	return &user, nil
}

// UpdatePassword updates the hashed password for a user document.
func (r *userRepository) UpdatePassword(ctx context.Context, id bson.ObjectID, hashedPassword string) error {
	filter := bson.M{"_id": id}
	update := bson.M{
		"$set": bson.M{
			"password":   hashedPassword,
			"updated_at": time.Now().UTC(),
		},
	}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// Delete removes a user document from the database.
func (r *userRepository) Delete(ctx context.Context, id bson.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// RecordFailedLogin increments the failed login attempt counter and returns the new count.
func (r *userRepository) RecordFailedLogin(ctx context.Context, id bson.ObjectID) (int, error) {
	filter := bson.M{"_id": id}
	update := bson.M{"$inc": bson.M{"failed_login_attempts": 1}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var user domain.User
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&user)
	if err != nil {
		return 0, fmt.Errorf("failed to record failed login: %w", err)
	}
	return user.FailedLoginAttempts, nil
}

// ClearFailedLogins resets the failed login attempt counter and lifts any active lock.
func (r *userRepository) ClearFailedLogins(ctx context.Context, id bson.ObjectID) error {
	filter := bson.M{"_id": id}
	update := bson.M{
		"$set":   bson.M{"failed_login_attempts": 0},
		"$unset": bson.M{"locked_until": ""},
	}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to clear failed logins: %w", err)
	}
	return nil
}

// LockAccount sets a lock expiry timestamp on the user account, blocking login until then.
func (r *userRepository) LockAccount(ctx context.Context, id bson.ObjectID, until time.Time) error {
	filter := bson.M{"_id": id}
	update := bson.M{"$set": bson.M{"locked_until": until}}
	_, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to lock account: %w", err)
	}
	return nil
}
