package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// FamilyMember represents a family member record associated with a Head of Family (HOF) user.
type FamilyMember struct {
	ID              bson.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID          bson.ObjectID `bson:"user_id" json:"user_id"`
	Name            string        `bson:"name" json:"name" binding:"required"`
	RelationWithHOF string        `bson:"relation_with_hof" json:"relation_with_hof" binding:"required"`
	Phone           string        `bson:"phone" json:"phone" binding:"required"`
	Email           string        `bson:"email,omitempty" json:"email,omitempty"`
	BloodGroup      string        `bson:"blood_group,omitempty" json:"blood_group,omitempty"`
	DateOfBirth     string        `bson:"date_of_birth" json:"date_of_birth" binding:"required"` // Format: YYYY-MM-DD
	// LICCustomerID is the customer identifier LIC (or another insurer) issues to this person —
	// stable across every policy they buy from that insurer, unlike the per-policy Policy Number.
	// Optional; one person's many Life/Health policies all point at this same family member record,
	// so this is the single place it's entered/edited (never duplicated onto individual policies).
	LICCustomerID string    `bson:"lic_customer_id,omitempty" json:"lic_customer_id,omitempty"`
	CreatedAt     time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time `bson:"updated_at" json:"updated_at"`
}

// CreateFamilyMemberDTO represents the payload for creating a new family member.
type CreateFamilyMemberDTO struct {
	Name            string `json:"name" binding:"required"`
	RelationWithHOF string `json:"relation_with_hof" binding:"required"`
	Phone           string `json:"phone" binding:"required"`
	Email           string `json:"email,omitempty"`
	BloodGroup      string `json:"blood_group,omitempty"`
	DateOfBirth     string `json:"date_of_birth" binding:"required"`
	LICCustomerID   string `json:"lic_customer_id,omitempty" example:"1234567890"`
}

// UpdateFamilyMemberDTO represents the payload for updating an existing family member.
type UpdateFamilyMemberDTO struct {
	Name            *string `json:"name,omitempty"`
	RelationWithHOF *string `json:"relation_with_hof,omitempty"`
	Phone           *string `json:"phone,omitempty"`
	Email           *string `json:"email,omitempty"`
	BloodGroup      *string `json:"blood_group,omitempty"`
	DateOfBirth     *string `json:"date_of_birth,omitempty"`
	LICCustomerID   *string `json:"lic_customer_id,omitempty" example:"1234567890"`
}

// FamilyMemberListResponse represents the response containing array of family members and count N.
type FamilyMemberListResponse struct {
	Total int64           `json:"total"`
	Data  []*FamilyMember `json:"data"`
}

// FamilyMemberRepository defines database CRUD operations for family members.
type FamilyMemberRepository interface {
	Create(ctx context.Context, member *FamilyMember) (*FamilyMember, error)
	FindAllByUserID(ctx context.Context, userID bson.ObjectID) ([]*FamilyMember, int64, error)
	FindByID(ctx context.Context, id bson.ObjectID) (*FamilyMember, error)
	Update(ctx context.Context, id, userID bson.ObjectID, update *UpdateFamilyMemberDTO) (*FamilyMember, error)
	Delete(ctx context.Context, id, userID bson.ObjectID) error
	DeleteAllByUserID(ctx context.Context, userID bson.ObjectID) error
}

// FamilyMemberService defines business logic operations for family members.
type FamilyMemberService interface {
	AddMember(ctx context.Context, userIDStr string, dto *CreateFamilyMemberDTO) (*FamilyMember, error)
	GetMyMembers(ctx context.Context, userIDStr string) (*FamilyMemberListResponse, error)
	GetMemberByID(ctx context.Context, idStr, userIDStr string) (*FamilyMember, error)
	UpdateMember(ctx context.Context, idStr, userIDStr string, dto *UpdateFamilyMemberDTO) (*FamilyMember, error)
	DeleteMember(ctx context.Context, idStr, userIDStr string) error
	GetMembersByUserIDAdmin(ctx context.Context, targetUserIDStr string) (*FamilyMemberListResponse, error)
	DeleteAllByUserID(ctx context.Context, userIDStr string) error
}
