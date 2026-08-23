package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Payment mode constants for PremiumDetails.PaymentMode.
const (
	PaymentModeYearly     = "Yearly"
	PaymentModeHalfYearly = "Half-Yearly"
	PaymentModeQuarterly  = "Quarterly"
	PaymentModeMonthly    = "Monthly"
)

// PolicyDetails holds the core policy attributes of a Life Insurance record.
type PolicyDetails struct {
	PolicyNo        string    `bson:"policy_no" json:"policy_no"`
	PlanName        string    `bson:"plan_name" json:"plan_name"`
	LifeInsuredName string    `bson:"life_insured_name" json:"life_insured_name"` // Cached from the linked family member
	NomineeName     string    `bson:"nominee_name" json:"nominee_name"`
	SumAssured      float64   `bson:"sum_assured" json:"sum_assured"`
	Term            int       `bson:"term" json:"term"` // Total policy duration in years
	PPT             int       `bson:"ppt" json:"ppt"`   // Premium Paying Term in years
	DOC             time.Time `bson:"doc" json:"doc"`   // Date of Commencement
	MaturityDate    time.Time `bson:"maturity_date" json:"maturity_date"`
}

// PremiumDetails holds the premium payment schedule of a Life Insurance record.
type PremiumDetails struct {
	InstallmentPremium float64   `bson:"installment_premium" json:"installment_premium"`
	NextDueDate        time.Time `bson:"next_due_date" json:"next_due_date"` // Indexed — powers premium-reminder/dashboard queries
	PaymentMode        string    `bson:"payment_mode" json:"payment_mode"`
}

// LifeInsurance represents a life insurance policy owned by a client (UserID) and mapped to a
// specific insured family member (FamilyMemberID).
type LifeInsurance struct {
	ID             bson.ObjectID  `bson:"_id,omitempty" json:"id"`
	UserID         bson.ObjectID  `bson:"user_id" json:"user_id"`
	FamilyMemberID bson.ObjectID  `bson:"family_member_id" json:"family_member_id"`
	CompanyName    string         `bson:"company_name" json:"company_name"`
	PolicyDetails  PolicyDetails  `bson:"policy_details" json:"policy_details"`
	PremiumDetails PremiumDetails `bson:"premium_details" json:"premium_details"`
	IsMapped       bool           `bson:"is_mapped" json:"is_mapped"` // Admin flag: formally mapped to their agency portfolio
	CreatedAt      time.Time      `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time      `bson:"updated_at" json:"updated_at"`
}

// LifeInsuranceWithCustomer represents a life insurance policy enriched with the owning
// customer's name and contact number — used for the Admin master list view so Admin/Super Admin
// can see every client's policies at a glance without looking each client up separately.
type LifeInsuranceWithCustomer struct {
	ID             bson.ObjectID  `bson:"_id" json:"id"`
	UserID         bson.ObjectID  `bson:"user_id" json:"user_id"`
	FamilyMemberID bson.ObjectID  `bson:"family_member_id" json:"family_member_id"`
	CompanyName    string         `bson:"company_name" json:"company_name"`
	CustomerName   string         `bson:"customer_name" json:"customer_name"`
	ContactNo      string         `bson:"contact_no" json:"contact_no"`
	PolicyDetails  PolicyDetails  `bson:"policy_details" json:"policy_details"`
	PremiumDetails PremiumDetails `bson:"premium_details" json:"premium_details"`
	IsMapped       bool           `bson:"is_mapped" json:"is_mapped"`
	CreatedAt      time.Time      `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time      `bson:"updated_at" json:"updated_at"`
}

// CreatePolicyDetailsDTO represents the policy-detail fields on the create payload.
type CreatePolicyDetailsDTO struct {
	PolicyNo     string    `json:"policy_no" binding:"required"`
	PlanName     string    `json:"plan_name" binding:"required"`
	NomineeName  string    `json:"nominee_name" binding:"required"`
	SumAssured   float64   `json:"sum_assured" binding:"required,gt=0"`
	Term         int       `json:"term" binding:"required,gt=0"`
	PPT          int       `json:"ppt" binding:"required,gt=0"`
	DOC          time.Time `json:"doc" binding:"required"`
	MaturityDate time.Time `json:"maturity_date" binding:"required"`
}

// CreatePremiumDetailsDTO represents the premium-schedule fields on the create payload.
type CreatePremiumDetailsDTO struct {
	InstallmentPremium float64   `json:"installment_premium" binding:"required,gt=0"`
	NextDueDate        time.Time `json:"next_due_date" binding:"required"`
	PaymentMode        string    `json:"payment_mode" binding:"required,oneof=Yearly Half-Yearly Quarterly Monthly"`
}

// CreateLifeInsuranceDTO represents the payload for adding a new Life Insurance policy.
// UserID is optional and only honored for admin/super_admin requesters via the "Select Member"
// flow — a client always gets the policy created under their own JWT account regardless of what
// (if anything) is sent here.
type CreateLifeInsuranceDTO struct {
	UserID         string                  `json:"user_id,omitempty" example:"64f1a2b3c4d5e6f7a8b9c0d1"`
	FamilyMemberID string                  `json:"family_member_id" binding:"required"`
	CompanyName    string                  `json:"company_name" binding:"required"`
	PolicyDetails  CreatePolicyDetailsDTO  `json:"policy_details" binding:"required"`
	PremiumDetails CreatePremiumDetailsDTO `json:"premium_details" binding:"required"`
	IsMapped       bool                    `json:"is_mapped,omitempty"`
}

// UpdateLifeInsuranceDTO represents the payload for partially updating an existing policy — only
// supplied (non-nil) fields are modified.
type UpdateLifeInsuranceDTO struct {
	FamilyMemberID *string `json:"family_member_id,omitempty"`
	CompanyName    *string `json:"company_name,omitempty"`

	PolicyNo     *string    `json:"policy_no,omitempty"`
	PlanName     *string    `json:"plan_name,omitempty"`
	NomineeName  *string    `json:"nominee_name,omitempty"`
	SumAssured   *float64   `json:"sum_assured,omitempty"`
	Term         *int       `json:"term,omitempty"`
	PPT          *int       `json:"ppt,omitempty"`
	DOC          *time.Time `json:"doc,omitempty"`
	MaturityDate *time.Time `json:"maturity_date,omitempty"`

	InstallmentPremium *float64   `json:"installment_premium,omitempty"`
	NextDueDate        *time.Time `json:"next_due_date,omitempty"`
	PaymentMode        *string    `json:"payment_mode,omitempty" binding:"omitempty,oneof=Yearly Half-Yearly Quarterly Monthly"`

	IsMapped *bool `json:"is_mapped,omitempty"`

	// LifeInsuredName is never client-settable (json:"-") — the service populates it
	// automatically when FamilyMemberID changes, keeping the cached name in sync.
	LifeInsuredName *string `json:"-"`
}

// LifeInsuranceListResponse represents a single client's list of life insurance policies.
type LifeInsuranceListResponse struct {
	Total int64            `json:"total"`
	Data  []*LifeInsurance `json:"data"`
}

// LifeInsuranceRepository defines database operations for life insurance policies.
type LifeInsuranceRepository interface {
	Create(ctx context.Context, policy *LifeInsurance) (*LifeInsurance, error)
	GetByID(ctx context.Context, id bson.ObjectID) (*LifeInsurance, error)
	GetByUserID(ctx context.Context, userID bson.ObjectID) ([]*LifeInsurance, int64, error)
	GetAll(ctx context.Context, page, limit int64, isMapped *bool) ([]*LifeInsuranceWithCustomer, int64, error)
	Update(ctx context.Context, id bson.ObjectID, dto *UpdateLifeInsuranceDTO) (*LifeInsurance, error)
	Delete(ctx context.Context, id bson.ObjectID) error
	DeleteAllByUserID(ctx context.Context, userID bson.ObjectID) error
}

// LifeInsuranceService defines business logic operations for life insurance policies.
type LifeInsuranceService interface {
	CreatePolicy(ctx context.Context, requesterRole, requesterID string, dto *CreateLifeInsuranceDTO) (*LifeInsurance, error)
	GetPolicyByID(ctx context.Context, requesterRole, requesterID, idStr string) (*LifeInsurance, error)
	GetMyPolicies(ctx context.Context, requesterID string) (*LifeInsuranceListResponse, error)
	GetAllPolicies(ctx context.Context, page, limit int64, isMapped *bool) ([]*LifeInsuranceWithCustomer, int64, error)
	UpdatePolicy(ctx context.Context, requesterRole, requesterID, idStr string, dto *UpdateLifeInsuranceDTO) (*LifeInsurance, error)
	DeletePolicy(ctx context.Context, requesterRole, requesterID, idStr string) error
	DeleteAllByUserID(ctx context.Context, userIDStr string) error
}
