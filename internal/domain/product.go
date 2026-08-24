package domain

import (
	"context"
	"io"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Product category enum values.
const (
	ProductCategoryLife       = "Life"
	ProductCategoryHealth     = "Health"
	ProductCategoryGeneral    = "General"
	ProductCategoryFD         = "FD"
	ProductCategoryMutualFund = "MutualFund"
)

// IsValidProductCategory reports whether s is one of the recognized product categories.
func IsValidProductCategory(s string) bool {
	switch s {
	case ProductCategoryLife, ProductCategoryHealth, ProductCategoryGeneral, ProductCategoryFD, ProductCategoryMutualFund:
		return true
	}
	return false
}

// Product represents a catalog entry describing an offering (policy/instrument type) the agency
// sells — fulfills the "KNOW ABOUT ALL PRODUCT" requirement by giving clients a browsable,
// read-only catalog while letting admin/super_admin manage the full lifecycle.
type Product struct {
	ID               bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Name             string        `bson:"name" json:"name"`
	Category         string        `bson:"category" json:"category"`
	Description      string        `bson:"description" json:"description"`
	KeyBenefits      []string      `bson:"key_benefits" json:"key_benefits"`
	BrochureURL      string        `bson:"brochure_url,omitempty" json:"brochure_url,omitempty"`
	BrochurePublicID string        `bson:"brochure_public_id,omitempty" json:"brochure_public_id,omitempty"`
	IsActive         bool          `bson:"is_active" json:"is_active"`
	CreatedAt        time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt        time.Time     `bson:"updated_at" json:"updated_at"`
}

// CreateProductDTO represents the payload for adding a new product. Populated by the handler from
// multipart form fields (the optional brochure file is handled separately as an io.Reader) —
// mirrors the E-Vault document upload pattern.
type CreateProductDTO struct {
	Name        string   `json:"name" form:"name" binding:"required"`
	Category    string   `json:"category" form:"category" binding:"required,oneof=Life Health General FD MutualFund"`
	Description string   `json:"description" form:"description"`
	KeyBenefits []string `json:"key_benefits,omitempty"`
	IsActive    *bool    `json:"is_active,omitempty"` // nil defaults to true (published) on create
}

// UpdateProductDTO represents the payload for partially updating an existing product — only
// non-nil fields are modified. BrochureURL/BrochurePublicID are populated internally by the
// service when a new brochure file is uploaded (never client-settable directly).
type UpdateProductDTO struct {
	Name        *string   `json:"name,omitempty" form:"name"`
	Category    *string   `json:"category,omitempty" form:"category" binding:"omitempty,oneof=Life Health General FD MutualFund"`
	Description *string   `json:"description,omitempty" form:"description"`
	KeyBenefits *[]string `json:"key_benefits,omitempty"`
	IsActive    *bool     `json:"is_active,omitempty"`

	BrochureURL      *string `json:"-"`
	BrochurePublicID *string `json:"-"`
}

// ProductListResponse represents a paginated list of catalog products.
type ProductListResponse struct {
	Total int64      `json:"total"`
	Data  []*Product `json:"data"`
}

// ProductRepository defines database operations for the product catalog.
type ProductRepository interface {
	Create(ctx context.Context, product *Product) (*Product, error)
	FindByID(ctx context.Context, id bson.ObjectID) (*Product, error)
	FindAll(ctx context.Context, page, limit int64, category string, isActive *bool) ([]*Product, int64, error)
	Update(ctx context.Context, id bson.ObjectID, dto *UpdateProductDTO) (*Product, error)
	Delete(ctx context.Context, id bson.ObjectID) error
}

// ProductService defines business logic operations for the product catalog.
type ProductService interface {
	CreateProduct(ctx context.Context, requesterRole string, dto *CreateProductDTO, file io.Reader, filename string) (*Product, error)
	GetProductByID(ctx context.Context, requesterRole, idStr string) (*Product, error)
	GetAllProducts(ctx context.Context, requesterRole string, page, limit int64, category string, isActive *bool) (*ProductListResponse, error)
	UpdateProduct(ctx context.Context, requesterRole, idStr string, dto *UpdateProductDTO, newFile io.Reader, filename string) (*Product, error)
	DeleteProduct(ctx context.Context, requesterRole, idStr string) error
}
