package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// User represents a user entity in the system.
type User struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Name      string        `bson:"name" json:"name" binding:"required"`
	Email     string        `bson:"email" json:"email" binding:"required,email"`
	Password  string        `bson:"password" json:"-"`
	Phone     string        `bson:"phone,omitempty" json:"phone,omitempty"`
	Role      string        `bson:"role" json:"role"`
	IsActive  bool          `bson:"is_active" json:"is_active"`
	CreatedAt time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time     `bson:"updated_at" json:"updated_at"`
}

// CreateUserRequest represents the request payload for creating a new user.
type CreateUserRequest struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	Phone    string `json:"phone,omitempty"`
}

// UpdateUserRequest represents the request payload for updating a user.
type UpdateUserRequest struct {
	Name  *string `json:"name,omitempty"`
	Email *string `json:"email,omitempty"`
	Phone *string `json:"phone,omitempty"`
}

// LoginRequest represents the request payload for user login.
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// UserResponse represents the response payload for a user (without sensitive data).
type UserResponse struct {
	ID        bson.ObjectID `json:"id"`
	Name      string        `json:"name"`
	Email     string        `json:"email"`
	Phone     string        `json:"phone,omitempty"`
	Role      string        `json:"role"`
	IsActive  bool          `json:"is_active"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// ToResponse converts a User entity to a UserResponse.
func (u *User) ToResponse() *UserResponse {
	return &UserResponse{
		ID:        u.ID,
		Name:      u.Name,
		Email:     u.Email,
		Phone:     u.Phone,
		Role:      u.Role,
		IsActive:  u.IsActive,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

// UserRepository defines the interface for user data access operations.
type UserRepository interface {
	Create(ctx context.Context, user *User) (*User, error)
	FindByID(ctx context.Context, id bson.ObjectID) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	FindAll(ctx context.Context, page, limit int64) ([]*User, int64, error)
	Update(ctx context.Context, id bson.ObjectID, update *UpdateUserRequest) (*User, error)
	Delete(ctx context.Context, id bson.ObjectID) error
}

// UserService defines the interface for user business logic operations.
type UserService interface {
	Register(ctx context.Context, req *CreateUserRequest) (*UserResponse, error)
	GetByID(ctx context.Context, id string) (*UserResponse, error)
	GetAll(ctx context.Context, page, limit int64) ([]*UserResponse, int64, error)
	Update(ctx context.Context, id string, req *UpdateUserRequest) (*UserResponse, error)
	Delete(ctx context.Context, id string) error
}
