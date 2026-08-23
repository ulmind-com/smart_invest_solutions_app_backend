package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// HealthPolicyDetails holds the core policy attributes of a Health Insurance record. Named
// distinctly from PolicyDetails (Life Insurance) since that type already exists in this package
// with a different field set — the JSON wire shape (policy_details) is unaffected.
type HealthPolicyDetails struct {
	PolicyNo           string    `bson:"policy_no" json:"policy_no"`
	PlanName           string    `bson:"plan_name" json:"plan_name"`
	PrimaryInsuredName string    `bson:"primary_insured_name" json:"primary_insured_name"` // Cached from the linked family member
	SumInsured         float64   `bson:"sum_insured" json:"sum_insured"`
	DOC                time.Time `bson:"doc" json:"doc"` // Date of Commencement
	ExpiryDate         time.Time `bson:"expiry_date" json:"expiry_date"`
}

// HealthPremiumDetails holds the premium payment schedule of a Health Insurance record.
type HealthPremiumDetails struct {
	InstallmentPremium float64   `bson:"installment_premium" json:"installment_premium"`
	NextDueDate        time.Time `bson:"next_due_date" json:"next_due_date"` // Indexed — powers renewal reminder queries
	PaymentMode        string    `bson:"payment_mode" json:"payment_mode"`
}

// HealthInsurance represents a health insurance policy owned by a client (UserID) and mapped to
// a specific primary insured family member (FamilyMemberID).
type HealthInsurance struct {
	ID             bson.ObjectID        `bson:"_id,omitempty" json:"id"`
	UserID         bson.ObjectID        `bson:"user_id" json:"user_id"`
	FamilyMemberID bson.ObjectID        `bson:"family_member_id" json:"family_member_id"`
	CompanyName    string               `bson:"company_name" json:"company_name"` // e.g. Star Health, Care Health
	PolicyDetails  HealthPolicyDetails  `bson:"policy_details" json:"policy_details"`
	PremiumDetails HealthPremiumDetails `bson:"premium_details" json:"premium_details"`
	IsMapped       bool                 `bson:"is_mapped" json:"is_mapped"` // Admin tracking flag — admin/super_admin only can change on update
	CreatedAt      time.Time            `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time            `bson:"updated_at" json:"updated_at"`
}

// HealthInsuranceWithCustomer represents a health insurance policy enriched with the owning
// customer's name and contact number — used for the Admin master list view.
type HealthInsuranceWithCustomer struct {
	ID             bson.ObjectID        `bson:"_id" json:"id"`
	UserID         bson.ObjectID        `bson:"user_id" json:"user_id"`
	FamilyMemberID bson.ObjectID        `bson:"family_member_id" json:"family_member_id"`
	CompanyName    string               `bson:"company_name" json:"company_name"`
	CustomerName   string               `bson:"customer_name" json:"customer_name"`
	ContactNo      string               `bson:"contact_no" json:"contact_no"`
	PolicyDetails  HealthPolicyDetails  `bson:"policy_details" json:"policy_details"`
	PremiumDetails HealthPremiumDetails `bson:"premium_details" json:"premium_details"`
	IsMapped       bool                 `bson:"is_mapped" json:"is_mapped"`
	CreatedAt      time.Time            `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time            `bson:"updated_at" json:"updated_at"`
}

// CreateHealthPolicyDetailsDTO represents the policy-detail fields on the create payload.
type CreateHealthPolicyDetailsDTO struct {
	PolicyNo   string    `json:"policy_no" binding:"required"`
	PlanName   string    `json:"plan_name" binding:"required"`
	SumInsured float64   `json:"sum_insured" binding:"required,gt=0"`
	DOC        time.Time `json:"doc" binding:"required"`
	ExpiryDate time.Time `json:"expiry_date" binding:"required"`
}

// CreateHealthPremiumDetailsDTO represents the premium-schedule fields on the create payload.
type CreateHealthPremiumDetailsDTO struct {
	InstallmentPremium float64   `json:"installment_premium" binding:"required,gt=0"`
	NextDueDate        time.Time `json:"next_due_date" binding:"required"`
	PaymentMode        string    `json:"payment_mode" binding:"required,oneof=Yearly Half-Yearly Quarterly Monthly"`
}

// CreateHealthInsuranceDTO represents the payload for adding a new Health Insurance policy.
// UserID is optional and only honored for admin/super_admin requesters — a client always gets
// the policy created under their own JWT account regardless of what's sent here.
type CreateHealthInsuranceDTO struct {
	UserID         string                        `json:"user_id,omitempty" example:"64f1a2b3c4d5e6f7a8b9c0d1"`
	FamilyMemberID string                        `json:"family_member_id" binding:"required"`
	CompanyName    string                        `json:"company_name" binding:"required"`
	PolicyDetails  CreateHealthPolicyDetailsDTO  `json:"policy_details" binding:"required"`
	PremiumDetails CreateHealthPremiumDetailsDTO `json:"premium_details" binding:"required"`
	IsMapped       bool                          `json:"is_mapped,omitempty"`
}

// UpdateHealthInsuranceDTO represents the payload for partially updating an existing policy —
// only supplied (non-nil) fields are modified. IsMapped is intentionally still bindable from JSON
// here (a client MAY send it), but the service layer strips it back to nil before persisting
// unless the requester is admin/super_admin — see healthInsuranceService.UpdatePolicy.
type UpdateHealthInsuranceDTO struct {
	FamilyMemberID *string `json:"family_member_id,omitempty"`
	CompanyName    *string `json:"company_name,omitempty"`

	PolicyNo   *string    `json:"policy_no,omitempty"`
	PlanName   *string    `json:"plan_name,omitempty"`
	SumInsured *float64   `json:"sum_insured,omitempty"`
	DOC        *time.Time `json:"doc,omitempty"`
	ExpiryDate *time.Time `json:"expiry_date,omitempty"`

	InstallmentPremium *float64   `json:"installment_premium,omitempty"`
	NextDueDate        *time.Time `json:"next_due_date,omitempty"`
	PaymentMode        *string    `json:"payment_mode,omitempty" binding:"omitempty,oneof=Yearly Half-Yearly Quarterly Monthly"`

	IsMapped *bool `json:"is_mapped,omitempty"`

	// PrimaryInsuredName is never client-settable (json:"-") — the service populates it
	// automatically when FamilyMemberID changes, keeping the cached name in sync.
	PrimaryInsuredName *string `json:"-"`
}

// HealthInsuranceListResponse represents a single client's list of health insurance policies.
type HealthInsuranceListResponse struct {
	Total int64              `json:"total"`
	Data  []*HealthInsurance `json:"data"`
}

// HealthInsuranceRepository defines database operations for health insurance policies.
type HealthInsuranceRepository interface {
	Create(ctx context.Context, policy *HealthInsurance) (*HealthInsurance, error)
	GetByID(ctx context.Context, id bson.ObjectID) (*HealthInsurance, error)
	GetByUserID(ctx context.Context, userID bson.ObjectID) ([]*HealthInsurance, int64, error)
	GetAll(ctx context.Context, page, limit int64, isMapped *bool) ([]*HealthInsuranceWithCustomer, int64, error)
	Update(ctx context.Context, id bson.ObjectID, dto *UpdateHealthInsuranceDTO) (*HealthInsurance, error)
	Delete(ctx context.Context, id bson.ObjectID) error
	DeleteAllByUserID(ctx context.Context, userID bson.ObjectID) error
}

// HealthInsuranceService defines business logic operations for health insurance policies.
type HealthInsuranceService interface {
	CreatePolicy(ctx context.Context, requesterRole, requesterID string, dto *CreateHealthInsuranceDTO) (*HealthInsurance, error)
	GetPolicyByID(ctx context.Context, requesterRole, requesterID, idStr string) (*HealthInsurance, error)
	GetMyPolicies(ctx context.Context, requesterID string) (*HealthInsuranceListResponse, error)
	GetAllPolicies(ctx context.Context, page, limit int64, isMapped *bool) ([]*HealthInsuranceWithCustomer, int64, error)
	UpdatePolicy(ctx context.Context, requesterRole, requesterID, idStr string, dto *UpdateHealthInsuranceDTO) (*HealthInsurance, error)
	DeletePolicy(ctx context.Context, requesterRole, requesterID, idStr string) error
	DeleteAllByUserID(ctx context.Context, userIDStr string) error
}
