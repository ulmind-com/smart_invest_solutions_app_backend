package service

import (
	"context"
	"fmt"
	"io"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// productBrochureFolder is the Cloudinary folder catalog brochures are stored under.
const productBrochureFolder = "smart_invest_products"

type productService struct {
	repo       domain.ProductRepository
	storageSvc StorageService
}

// NewProductService creates a new instance of ProductService.
func NewProductService(repo domain.ProductRepository, storageSvc StorageService) domain.ProductService {
	return &productService{repo: repo, storageSvc: storageSvc}
}

// isAgencyRole reports whether the requester is staff (admin/super_admin) — such requesters get
// full CRUD access and may view inactive/unpublished products. Any other role (client, advisor)
// is treated as a read-only catalog browser restricted to published (IsActive == true) products.
func isAgencyRole(requesterRole string) bool {
	return requesterRole == domain.RoleAdmin || requesterRole == domain.RoleSuperAdmin
}

// CreateProduct adds a new catalog product. Admin/super_admin only. If a brochure file is
// supplied it is uploaded to Cloudinary first; IsActive defaults to true (published) when omitted.
func (s *productService) CreateProduct(ctx context.Context, requesterRole string, dto *domain.CreateProductDTO, file io.Reader, filename string) (*domain.Product, error) {
	if !isAgencyRole(requesterRole) {
		return nil, fmt.Errorf("access denied: only admin can create products")
	}

	if !domain.IsValidProductCategory(dto.Category) {
		return nil, fmt.Errorf("invalid category: %s", dto.Category)
	}

	isActive := true
	if dto.IsActive != nil {
		isActive = *dto.IsActive
	}

	product := &domain.Product{
		Name:        dto.Name,
		Category:    dto.Category,
		Description: dto.Description,
		KeyBenefits: dto.KeyBenefits,
		IsActive:    isActive,
	}

	if file != nil {
		uploadRes, err := s.storageSvc.UploadDocumentWithCompression(ctx, file, productBrochureFolder)
		if err != nil {
			return nil, fmt.Errorf("failed to upload brochure to Cloudinary: %w", err)
		}
		product.BrochureURL = uploadRes.SecureURL
		product.BrochurePublicID = uploadRes.PublicID
	}

	return s.repo.Create(ctx, product)
}

// GetProductByID retrieves a single product. Client/advisor requesters may only see published
// (IsActive == true) products — an inactive product is reported as not found to avoid leaking its
// existence. Admin/super_admin may retrieve any product regardless of status.
func (s *productService) GetProductByID(ctx context.Context, requesterRole, idStr string) (*domain.Product, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid product ID format: %w", err)
	}

	product, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if !isAgencyRole(requesterRole) && !product.IsActive {
		return nil, fmt.Errorf("product not found")
	}

	return product, nil
}

// GetAllProducts returns a paginated view of the catalog, optionally filtered by category.
// Client/advisor requesters are always forced to the published (IsActive == true) subset,
// regardless of what isActive is passed in; admin/super_admin may pass nil to see every product
// (including inactive/unpublished ones) or a specific true/false filter.
func (s *productService) GetAllProducts(ctx context.Context, requesterRole string, page, limit int64, category string, isActive *bool) (*domain.ProductListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	if category != "" && !domain.IsValidProductCategory(category) {
		return nil, fmt.Errorf("invalid category: %s", category)
	}

	if !isAgencyRole(requesterRole) {
		published := true
		isActive = &published
	}

	products, total, err := s.repo.FindAll(ctx, page, limit, category, isActive)
	if err != nil {
		return nil, err
	}

	return &domain.ProductListResponse{Total: total, Data: products}, nil
}

// UpdateProduct modifies an existing product. Admin/super_admin only. If a new brochure file is
// supplied, the OLD brochure is purged from Cloudinary first, then the new file is uploaded and
// its URL/PublicID are written onto the DTO before persisting.
func (s *productService) UpdateProduct(ctx context.Context, requesterRole, idStr string, dto *domain.UpdateProductDTO, newFile io.Reader, filename string) (*domain.Product, error) {
	if !isAgencyRole(requesterRole) {
		return nil, fmt.Errorf("access denied: only admin can update products")
	}

	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid product ID format: %w", err)
	}

	if dto.Category != nil && !domain.IsValidProductCategory(*dto.Category) {
		return nil, fmt.Errorf("invalid category: %s", *dto.Category)
	}

	existing, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if newFile != nil {
		if existing.BrochurePublicID != "" {
			_ = s.storageSvc.DeleteImage(ctx, existing.BrochurePublicID)
		}

		uploadRes, err := s.storageSvc.UploadDocumentWithCompression(ctx, newFile, productBrochureFolder)
		if err != nil {
			return nil, fmt.Errorf("failed to upload new brochure file: %w", err)
		}
		dto.BrochureURL = &uploadRes.SecureURL
		dto.BrochurePublicID = &uploadRes.PublicID
	}

	return s.repo.Update(ctx, id, dto)
}

// DeleteProduct removes a product from MongoDB and cascade-deletes its brochure asset from
// Cloudinary. Admin/super_admin only.
func (s *productService) DeleteProduct(ctx context.Context, requesterRole, idStr string) error {
	if !isAgencyRole(requesterRole) {
		return fmt.Errorf("access denied: only admin can delete products")
	}

	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return fmt.Errorf("invalid product ID format: %w", err)
	}

	existing, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if existing.BrochurePublicID != "" {
		if err := s.storageSvc.DeleteImage(ctx, existing.BrochurePublicID); err != nil {
			log.Warn().Err(err).Str("product_id", idStr).Msg("failed to purge brochure from Cloudinary")
		}
	}

	return s.repo.Delete(ctx, id)
}
