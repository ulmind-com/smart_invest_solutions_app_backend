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
	// accessSvc decides how much of the catalog each client sees. The catalog is platform-wide, so
	// this is the only thing that makes one client's Products tab differ from another's.
	accessSvc domain.ClientProductAccessService
	// accessRepo is used only to clean up after a deleted product; visibility is always read
	// through accessSvc, which is where the rules live.
	accessRepo domain.ClientProductAccessRepository
}

// NewProductService creates a new instance of ProductService.
func NewProductService(
	repo domain.ProductRepository,
	storageSvc StorageService,
	accessSvc domain.ClientProductAccessService,
	accessRepo domain.ClientProductAccessRepository,
) domain.ProductService {
	return &productService{repo: repo, storageSvc: storageSvc, accessSvc: accessSvc, accessRepo: accessRepo}
}

// isAgencyRole reports whether the requester is staff (admin/super_admin) — such requesters may
// view inactive/unpublished products (but not necessarily write them; see isSuperAdmin). Any other
// role (client, advisor) is treated as a read-only catalog browser restricted to published
// (IsActive == true) products.
func isAgencyRole(requesterRole string) bool {
	return requesterRole == domain.RoleAdmin || requesterRole == domain.RoleSuperAdmin
}

// isSuperAdmin reports whether the requester may create, update, or delete a catalog product. A
// plain admin can browse the full catalog (including drafts) but cannot modify it — only a Super
// Admin curates the product list.
func isSuperAdmin(requesterRole string) bool {
	return requesterRole == domain.RoleSuperAdmin
}

// CreateProduct adds a new catalog product. Super Admin only. If a brochure file is supplied it is
// uploaded to Cloudinary first; IsActive defaults to true (published) when omitted.
func (s *productService) CreateProduct(ctx context.Context, requesterRole string, dto *domain.CreateProductDTO, file io.Reader, filename string) (*domain.Product, error) {
	if !isSuperAdmin(requesterRole) {
		return nil, fmt.Errorf("access denied: only a super admin can create products")
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
// (IsActive == true) products, and only those their admin has left visible to them — anything else
// is reported as not found to avoid leaking its existence. Admin/super_admin may retrieve any
// product regardless of status.
//
// This check matters as much as the list filter: without it, a product hidden from a client would
// still open from a link, a cached list or a hand-written request.
func (s *productService) GetProductByID(ctx context.Context, requesterRole, requesterID, idStr string) (*domain.Product, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid product ID format: %w", err)
	}

	product, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if !isAgencyRole(requesterRole) {
		if !product.IsActive {
			return nil, fmt.Errorf("product not found")
		}
		visibility, err := s.visibilityFor(ctx, requesterRole, requesterID)
		if err != nil {
			return nil, err
		}
		if !visibility.Allows(id) {
			return nil, fmt.Errorf("product not found")
		}
	}

	return product, nil
}

// GetAllProducts returns a paginated view of the catalog, optionally filtered by category.
// Client/advisor requesters are always forced to the published (IsActive == true) subset,
// regardless of what isActive is passed in; admin/super_admin may pass nil to see every product
// (including inactive/unpublished ones) or a specific true/false filter.
func (s *productService) GetAllProducts(ctx context.Context, requesterRole, requesterID string, page, limit int64, category string, isActive *bool) (*domain.ProductListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	if category != "" && !domain.IsValidProductCategory(category) {
		return nil, fmt.Errorf("invalid category: %s", category)
	}

	query := domain.ProductQuery{Page: page, Limit: limit, Category: category, IsActive: isActive}

	if !isAgencyRole(requesterRole) {
		published := true
		query.IsActive = &published

		visibility, err := s.visibilityFor(ctx, requesterRole, requesterID)
		if err != nil {
			return nil, err
		}
		query.Restricted = visibility.Restricted
		query.AllowedIDs = visibility.AllowedIDs
	}

	products, total, err := s.repo.FindAll(ctx, query)
	if err != nil {
		return nil, err
	}

	return &domain.ProductListResponse{Total: total, Data: products}, nil
}

// visibilityFor resolves how much of the catalog this caller may see.
//
// Fails closed if the access service was never wired: a missing dependency must not quietly turn
// into "everyone sees everything", which is the one outcome nobody would notice in testing.
func (s *productService) visibilityFor(ctx context.Context, requesterRole, requesterID string) (domain.ProductVisibility, error) {
	if s.accessSvc == nil {
		return domain.ProductVisibility{}, fmt.Errorf("catalog access is unavailable")
	}
	return s.accessSvc.ResolveVisibility(ctx, requesterRole, requesterID)
}

// UpdateProduct modifies an existing product. Super Admin only. If a new brochure file is
// supplied, the OLD brochure is purged from Cloudinary first, then the new file is uploaded and
// its URL/PublicID are written onto the DTO before persisting.
func (s *productService) UpdateProduct(ctx context.Context, requesterRole, idStr string, dto *domain.UpdateProductDTO, newFile io.Reader, filename string) (*domain.Product, error) {
	if !isSuperAdmin(requesterRole) {
		return nil, fmt.Errorf("access denied: only a super admin can update products")
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

	// Upload the replacement first and only purge the old brochure once the product points at the
	// new one — deleting first meant a failed upload left the product linking to a deleted file.
	oldPublicID := ""
	if newFile != nil {
		uploadRes, err := s.storageSvc.UploadDocumentWithCompression(ctx, newFile, productBrochureFolder)
		if err != nil {
			return nil, fmt.Errorf("failed to upload new brochure file: %w", err)
		}
		dto.BrochureURL = &uploadRes.SecureURL
		dto.BrochurePublicID = &uploadRes.PublicID
		oldPublicID = existing.BrochurePublicID
	}

	updated, err := s.repo.Update(ctx, id, dto)
	if err != nil {
		if dto.BrochurePublicID != nil {
			_ = s.storageSvc.DeleteImage(ctx, *dto.BrochurePublicID)
		}
		return nil, err
	}

	if oldPublicID != "" {
		if err := s.storageSvc.DeleteImage(ctx, oldPublicID); err != nil {
			log.Warn().Err(err).Str("product_id", idStr).Msg("failed to purge replaced brochure from Cloudinary")
		}
	}

	return updated, nil
}

// DeleteProduct removes a product from MongoDB and cascade-deletes its brochure asset from
// Cloudinary. Super Admin only.
func (s *productService) DeleteProduct(ctx context.Context, requesterRole, idStr string) error {
	if !isSuperAdmin(requesterRole) {
		return fmt.Errorf("access denied: only a super admin can delete products")
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

	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}

	// Drop the product from every client allow-list that named it. Reads already tolerate a missing
	// product, but a lingering ID would make the admin screen report a client as seeing more
	// products than they can — and would silently come back to life if the ID were ever reused.
	if s.accessRepo != nil {
		if err := s.accessRepo.RemoveProductFromAll(ctx, id); err != nil {
			log.Warn().Err(err).Str("product_id", idStr).Msg("failed to drop the deleted product from client access lists")
		}
	}

	return nil
}
