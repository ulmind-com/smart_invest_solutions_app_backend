package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// FixedDeposit represents a Fixed Deposit / Postal savings instrument (e.g. bank FD, Post Office
// MIS, SBI Tax Saver) owned by a client (UserID) and mapped to a specific family member as the
// 1st Holder (FamilyMemberID).
type FixedDeposit struct {
	ID               bson.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID           bson.ObjectID `bson:"user_id" json:"user_id"`
	FamilyMemberID   bson.ObjectID `bson:"family_member_id" json:"family_member_id"` // 1st Holder
	FDNumber         string        `bson:"fd_number" json:"fd_number"`
	FDName           string        `bson:"fd_name" json:"fd_name"`
	CompanyName      string        `bson:"company_name" json:"company_name"` // Bank / Institution
	PrincipalAmount  float64       `bson:"principal_amount" json:"principal_amount"`
	MaturityAmount   float64       `bson:"maturity_amount" json:"maturity_amount"`
	Term             int           `bson:"term_months" json:"term_months"` // Duration in months
	OpeningDate      time.Time     `bson:"opening_date" json:"opening_date"`
	MaturityDate     time.Time     `bson:"maturity_date" json:"maturity_date"`
	NomineeName      string        `bson:"nominee_name" json:"nominee_name"`
	SecondHolderName string        `bson:"second_holder_name,omitempty" json:"second_holder_name,omitempty"`
	AccountType      string        `bson:"account_type" json:"account_type"`
	Address          string        `bson:"address" json:"address"`     // Branch / Post office address
	IsMapped         bool          `bson:"is_mapped" json:"is_mapped"` // Admin tracking flag — admin/super_admin only can change on update
	CreatedAt        time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt        time.Time     `bson:"updated_at" json:"updated_at"`
}

// FixedDepositWithCustomer represents a Fixed Deposit enriched with the owning customer's name
// and contact number — used for the Admin master list view.
type FixedDepositWithCustomer struct {
	ID               bson.ObjectID `bson:"_id" json:"id"`
	UserID           bson.ObjectID `bson:"user_id" json:"user_id"`
	FamilyMemberID   bson.ObjectID `bson:"family_member_id" json:"family_member_id"`
	CustomerName     string        `bson:"customer_name" json:"customer_name"`
	ContactNo        string        `bson:"contact_no" json:"contact_no"`
	FDNumber         string        `bson:"fd_number" json:"fd_number"`
	FDName           string        `bson:"fd_name" json:"fd_name"`
	CompanyName      string        `bson:"company_name" json:"company_name"`
	PrincipalAmount  float64       `bson:"principal_amount" json:"principal_amount"`
	MaturityAmount   float64       `bson:"maturity_amount" json:"maturity_amount"`
	Term             int           `bson:"term_months" json:"term_months"`
	OpeningDate      time.Time     `bson:"opening_date" json:"opening_date"`
	MaturityDate     time.Time     `bson:"maturity_date" json:"maturity_date"`
	NomineeName      string        `bson:"nominee_name" json:"nominee_name"`
	SecondHolderName string        `bson:"second_holder_name,omitempty" json:"second_holder_name,omitempty"`
	AccountType      string        `bson:"account_type" json:"account_type"`
	Address          string        `bson:"address" json:"address"`
	IsMapped         bool          `bson:"is_mapped" json:"is_mapped"`
	CreatedAt        time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt        time.Time     `bson:"updated_at" json:"updated_at"`
}

// CreateFixedDepositDTO represents the payload for adding a new Fixed Deposit. UserID is optional
// and only honored for admin/super_admin requesters — a client always gets the FD created under
// their own JWT account regardless of what's sent here.
type CreateFixedDepositDTO struct {
	UserID           string    `json:"user_id,omitempty" example:"64f1a2b3c4d5e6f7a8b9c0d1"`
	FamilyMemberID   string    `json:"family_member_id" binding:"required"`
	FDNumber         string    `json:"fd_number" binding:"required"`
	FDName           string    `json:"fd_name" binding:"required"`
	CompanyName      string    `json:"company_name" binding:"required"`
	PrincipalAmount  float64   `json:"principal_amount" binding:"required,gt=0"`
	MaturityAmount   float64   `json:"maturity_amount" binding:"required,gt=0"`
	Term             int       `json:"term_months" binding:"required,gt=0"`
	OpeningDate      time.Time `json:"opening_date" binding:"required"`
	MaturityDate     time.Time `json:"maturity_date" binding:"required"`
	NomineeName      string    `json:"nominee_name" binding:"required"`
	SecondHolderName string    `json:"second_holder_name,omitempty"`
	AccountType      string    `json:"account_type" binding:"required"`
	Address          string    `json:"address" binding:"required"`
	IsMapped         bool      `json:"is_mapped,omitempty"`
}

// UpdateFixedDepositDTO represents the payload for partially updating an existing Fixed Deposit —
// only supplied (non-nil) fields are modified. IsMapped is intentionally still bindable from JSON
// here (a client MAY send it), but the service layer strips it back to nil before persisting
// unless the requester is admin/super_admin — see fixedDepositService.UpdateFD.
type UpdateFixedDepositDTO struct {
	FamilyMemberID   *string    `json:"family_member_id,omitempty"`
	FDNumber         *string    `json:"fd_number,omitempty"`
	FDName           *string    `json:"fd_name,omitempty"`
	CompanyName      *string    `json:"company_name,omitempty"`
	PrincipalAmount  *float64   `json:"principal_amount,omitempty"`
	MaturityAmount   *float64   `json:"maturity_amount,omitempty"`
	Term             *int       `json:"term_months,omitempty"`
	OpeningDate      *time.Time `json:"opening_date,omitempty"`
	MaturityDate     *time.Time `json:"maturity_date,omitempty"`
	NomineeName      *string    `json:"nominee_name,omitempty"`
	SecondHolderName *string    `json:"second_holder_name,omitempty"`
	AccountType      *string    `json:"account_type,omitempty"`
	Address          *string    `json:"address,omitempty"`
	IsMapped         *bool      `json:"is_mapped,omitempty"`
}

// FixedDepositListResponse represents a single client's list of Fixed Deposits.
type FixedDepositListResponse struct {
	Total int64           `json:"total"`
	Data  []*FixedDeposit `json:"data"`
}

// FixedDepositRepository defines database operations for Fixed Deposits.
type FixedDepositRepository interface {
	Create(ctx context.Context, fd *FixedDeposit) (*FixedDeposit, error)
	GetByID(ctx context.Context, id bson.ObjectID) (*FixedDeposit, error)
	GetByUserID(ctx context.Context, userID bson.ObjectID) ([]*FixedDeposit, int64, error)
	GetAll(ctx context.Context, page, limit int64, isMapped *bool) ([]*FixedDepositWithCustomer, int64, error)
	Update(ctx context.Context, id bson.ObjectID, dto *UpdateFixedDepositDTO) (*FixedDeposit, error)
	Delete(ctx context.Context, id bson.ObjectID) error
	DeleteAllByUserID(ctx context.Context, userID bson.ObjectID) error
}

// FixedDepositService defines business logic operations for Fixed Deposits.
type FixedDepositService interface {
	CreateFD(ctx context.Context, requesterRole, requesterID string, dto *CreateFixedDepositDTO) (*FixedDeposit, error)
	GetFDByID(ctx context.Context, requesterRole, requesterID, idStr string) (*FixedDeposit, error)
	GetMyFDs(ctx context.Context, requesterID string) (*FixedDepositListResponse, error)
	GetAllFDs(ctx context.Context, page, limit int64, isMapped *bool) ([]*FixedDepositWithCustomer, int64, error)
	UpdateFD(ctx context.Context, requesterRole, requesterID, idStr string, dto *UpdateFixedDepositDTO) (*FixedDeposit, error)
	DeleteFD(ctx context.Context, requesterRole, requesterID, idStr string) error
	DeleteAllByUserID(ctx context.Context, userIDStr string) error
}
