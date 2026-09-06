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
	ID                  bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Name                string        `bson:"name" json:"name" binding:"required"`
	Email               string        `bson:"email" json:"email" binding:"required,email"`
	Phone               string        `bson:"phone" json:"phone" binding:"required"`
	Notes               string        `bson:"notes,omitempty" json:"notes,omitempty"`
	AppliedReferralCode string        `bson:"applied_referral_code,omitempty" json:"applied_referral_code,omitempty"`
	// AppliedAgencyID is the AdminID (e.g. "ADM-7F3K9Q") the applicant supplied to say which
	// agency/admin they want to be managed by. Validated against a real admin account at
	// submission time. Empty means unassigned — visible only to a super_admin.
	AppliedAgencyID string    `bson:"applied_agency_id,omitempty" json:"applied_agency_id,omitempty"`
	Status          string    `bson:"status" json:"status"` // PENDING, APPROVED, REJECTED
	AdminNotes      string    `bson:"admin_notes,omitempty" json:"admin_notes,omitempty"`
	CreatedAt       time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt       time.Time `bson:"updated_at" json:"updated_at"`
}

// CreateAccessRequestDTO represents the payload when a client requests access.
type CreateAccessRequestDTO struct {
	Name                string `json:"name" binding:"required"`
	Email               string `json:"email" binding:"required,email"`
	Phone               string `json:"phone" binding:"required"`
	Notes               string `json:"notes,omitempty"`
	AppliedReferralCode string `json:"applied_referral_code,omitempty"`
	// AgencyID is the Agency ID (an admin's AdminID, e.g. "ADM-7F3K9Q") the applicant was given by
	// their agent/advisor. When supplied it must match a real admin account, or submission fails.
	AgencyID string `json:"agency_id,omitempty" example:"ADM-7F3K9Q"`
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
	// FindAll returns paginated requests, optionally filtered by status and/or agencyID (matches
	// AppliedAgencyID — pass empty to skip that filter, i.e. the platform-wide view).
	FindAll(ctx context.Context, status, agencyID string, page, limit int64) ([]*AccessRequest, int64, error)
	UpdateStatus(ctx context.Context, id bson.ObjectID, status string, adminNotes string) (*AccessRequest, error)
	UpdateDetailsAndStatus(ctx context.Context, id bson.ObjectID, name, phone, notes, appliedReferralCode, appliedAgencyID, status string) (*AccessRequest, error)
}

// AccessRequestService defines business logic methods for AccessRequests. Every method that takes
// requesterRole/requesterID scopes its result (or ownership check) to the caller: a super_admin has
// unrestricted access; a plain admin is limited to requests whose AppliedAgencyID matches their own
// AdminID, and a request with no agency at all is visible only to a super_admin.
type AccessRequestService interface {
	SubmitRequest(ctx context.Context, req *CreateAccessRequestDTO) (*AccessRequest, error)
	GetAllRequests(ctx context.Context, requesterRole, requesterID, status string, page, limit int64) ([]*AccessRequest, int64, error)
	GetRequestByID(ctx context.Context, requesterRole, requesterID, id string) (*AccessRequest, error)
	ApproveRequest(ctx context.Context, requesterRole, requesterID, id string, dto *ApproveAccessRequestDTO) (*UserResponse, error)
	RejectRequest(ctx context.Context, requesterRole, requesterID, id string, dto *RejectAccessRequestDTO) (*AccessRequest, error)
}
