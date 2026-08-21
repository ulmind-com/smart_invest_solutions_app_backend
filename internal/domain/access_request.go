package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// AccessRequest Status constants
const (
	AccessStatusPending  = "PENDING"
	AccessStatusApproved = "APPROVED"
	AccessStatusRejected = "REJECTED"
)

// AccessRequest represents a client's request for platform access.
type AccessRequest struct {
	ID         bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Name       string        `bson:"name" json:"name" binding:"required"`
	Email      string        `bson:"email" json:"email" binding:"required,email"`
	Phone      string        `bson:"phone" json:"phone" binding:"required"`
	Notes      string        `bson:"notes,omitempty" json:"notes,omitempty"`
	Status     string        `bson:"status" json:"status"` // PENDING, APPROVED, REJECTED
	AdminNotes string        `bson:"admin_notes,omitempty" json:"admin_notes,omitempty"`
	CreatedAt  time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt  time.Time     `bson:"updated_at" json:"updated_at"`
}

// CreateAccessRequestDTO represents the payload when a client requests access.
type CreateAccessRequestDTO struct {
	Name  string `json:"name" binding:"required"`
	Email string `json:"email" binding:"required,email"`
	Phone string `json:"phone" binding:"required"`
	Notes string `json:"notes,omitempty"`
}

// ApproveAccessRequestDTO represents payload for approving a request.
type ApproveAccessRequestDTO struct {
	AdminNotes string `json:"admin_notes,omitempty"`
}

// RejectAccessRequestDTO represents payload for rejecting a request.
type RejectAccessRequestDTO struct {
	Reason string `json:"reason,omitempty"`
}

// AccessRequestRepository defines data access methods for AccessRequests.
type AccessRequestRepository interface {
	Create(ctx context.Context, req *AccessRequest) (*AccessRequest, error)
	FindByID(ctx context.Context, id bson.ObjectID) (*AccessRequest, error)
	FindByEmail(ctx context.Context, email string) (*AccessRequest, error)
	FindAll(ctx context.Context, status string, page, limit int64) ([]*AccessRequest, int64, error)
	UpdateStatus(ctx context.Context, id bson.ObjectID, status string, adminNotes string) (*AccessRequest, error)
}

// AccessRequestService defines business logic methods for AccessRequests.
type AccessRequestService interface {
	SubmitRequest(ctx context.Context, req *CreateAccessRequestDTO) (*AccessRequest, error)
	GetAllRequests(ctx context.Context, status string, page, limit int64) ([]*AccessRequest, int64, error)
	GetRequestByID(ctx context.Context, id string) (*AccessRequest, error)
	ApproveRequest(ctx context.Context, id string, dto *ApproveAccessRequestDTO) (*UserResponse, error)
	RejectRequest(ctx context.Context, id string, dto *RejectAccessRequestDTO) (*AccessRequest, error)
}
