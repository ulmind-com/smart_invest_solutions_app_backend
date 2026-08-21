package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// GeneralInsurance represents a vehicle/general insurance policy record associated with a user.
type GeneralInsurance struct {
	ID             bson.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID         bson.ObjectID `bson:"user_id" json:"user_id"`
	VehicleNo      string        `bson:"vehicle_no" json:"vehicle_no" binding:"required"`
	PolicyNo       string        `bson:"policy_no" json:"policy_no" binding:"required"`
	DateOfExpiry   string        `bson:"date_of_expiry" json:"date_of_expiry" binding:"required"` // Format: YYYY-MM-DD
	CompanyName    string        `bson:"company_name" json:"company_name" binding:"required"`
	AdvisorName    string        `bson:"advisor_name,omitempty" json:"advisor_name,omitempty"`
	AdvisorContact string        `bson:"advisor_contact,omitempty" json:"advisor_contact,omitempty"`
	CreatedAt      time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time     `bson:"updated_at" json:"updated_at"`
}

// CreateGeneralInsuranceDTO represents the payload for adding a new General Insurance policy.
type CreateGeneralInsuranceDTO struct {
	VehicleNo      string `json:"vehicle_no" binding:"required"`
	PolicyNo       string `json:"policy_no" binding:"required"`
	DateOfExpiry   string `json:"date_of_expiry" binding:"required"`
	CompanyName    string `json:"company_name" binding:"required"`
	AdvisorName    string `json:"advisor_name,omitempty"`
	AdvisorContact string `json:"advisor_contact,omitempty"`
}

// UpdateGeneralInsuranceDTO represents the payload for updating an existing General Insurance policy.
type UpdateGeneralInsuranceDTO struct {
	VehicleNo      *string `json:"vehicle_no,omitempty"`
	PolicyNo       *string `json:"policy_no,omitempty"`
	DateOfExpiry   *string `json:"date_of_expiry,omitempty"`
	CompanyName    *string `json:"company_name,omitempty"`
	AdvisorName    *string `json:"advisor_name,omitempty"`
	AdvisorContact *string `json:"advisor_contact,omitempty"`
}

// GeneralInsuranceListResponse represents the paginated / count response for general insurances.
type GeneralInsuranceListResponse struct {
	Total int64               `json:"total"`
	Data  []*GeneralInsurance `json:"data"`
}

// GeneralInsuranceRepository defines database operations for general insurances.
type GeneralInsuranceRepository interface {
	Create(ctx context.Context, policy *GeneralInsurance) (*GeneralInsurance, error)
	FindAllByUserID(ctx context.Context, userID bson.ObjectID) ([]*GeneralInsurance, int64, error)
	FindByID(ctx context.Context, id bson.ObjectID) (*GeneralInsurance, error)
	Update(ctx context.Context, id, userID bson.ObjectID, update *UpdateGeneralInsuranceDTO) (*GeneralInsurance, error)
	Delete(ctx context.Context, id, userID bson.ObjectID) error
}

// GeneralInsuranceService defines business logic operations for general insurances.
type GeneralInsuranceService interface {
	AddInsurance(ctx context.Context, userIDStr string, dto *CreateGeneralInsuranceDTO) (*GeneralInsurance, error)
	GetMyInsurances(ctx context.Context, userIDStr string) (*GeneralInsuranceListResponse, error)
	GetInsuranceByID(ctx context.Context, idStr, userIDStr string) (*GeneralInsurance, error)
	UpdateInsurance(ctx context.Context, idStr, userIDStr string, dto *UpdateGeneralInsuranceDTO) (*GeneralInsurance, error)
	DeleteInsurance(ctx context.Context, idStr, userIDStr string) error
	GetInsurancesByUserIDAdmin(ctx context.Context, targetUserIDStr string) (*GeneralInsuranceListResponse, error)
}
