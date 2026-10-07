package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// maxSelectedProducts caps one client's allow-list. The catalog is a curated shortlist, not a
// marketplace, so a request far beyond this is a malformed client rather than a real selection.
const maxSelectedProducts = 500

type clientProductAccessService struct {
	accessRepo  domain.ClientProductAccessRepository
	userRepo    domain.UserRepository
	productRepo domain.ProductRepository
}

// NewClientProductAccessService creates the per-client catalog access service.
func NewClientProductAccessService(
	accessRepo domain.ClientProductAccessRepository,
	userRepo domain.UserRepository,
	productRepo domain.ProductRepository,
) domain.ClientProductAccessService {
	return &clientProductAccessService{accessRepo: accessRepo, userRepo: userRepo, productRepo: productRepo}
}

// GetForClient returns the client's current catalog setting.
func (s *clientProductAccessService) GetForClient(ctx context.Context, requesterRole, requesterID, targetUserID string) (*domain.ClientProductAccessDTO, error) {
	target, err := s.loadEditableClient(ctx, requesterRole, requesterID, targetUserID)
	if err != nil {
		return nil, err
	}

	access, err := s.accessRepo.FindByUserID(ctx, target.ID)
	if err != nil {
		return nil, err
	}

	return s.toDTO(ctx, target.ID, access)
}

// SetForClient replaces the client's catalog setting.
//
// Every submitted product ID is checked against the catalog. A silently dropped unknown ID would be
// the worst possible failure here: the admin would believe they had granted a product, and the
// client would never see it with nothing on screen to explain why.
func (s *clientProductAccessService) SetForClient(ctx context.Context, requesterRole, requesterID, targetUserID string, dto *domain.SetClientProductAccessDTO) (*domain.ClientProductAccessDTO, error) {
	target, err := s.loadEditableClient(ctx, requesterRole, requesterID, targetUserID)
	if err != nil {
		return nil, err
	}

	mode := strings.TrimSpace(strings.ToLower(dto.Mode))
	if !domain.IsValidProductAccessMode(mode) {
		return nil, fmt.Errorf("invalid mode: %s", dto.Mode)
	}

	var productIDs []bson.ObjectID
	if mode == domain.ProductAccessSelected {
		productIDs, err = s.resolveProductIDs(ctx, dto.ProductIDs)
		if err != nil {
			return nil, err
		}
	}

	requesterObjID, err := bson.ObjectIDFromHex(requesterID)
	if err != nil {
		return nil, fmt.Errorf("invalid requester ID format: %w", err)
	}
	requester, err := s.userRepo.FindByID(ctx, requesterObjID)
	if err != nil || requester == nil {
		return nil, fmt.Errorf("requester not found")
	}

	saved, err := s.accessRepo.Upsert(ctx, &domain.ClientProductAccess{
		UserID:        target.ID,
		Mode:          mode,
		ProductIDs:    productIDs,
		UpdatedByID:   requester.ID,
		UpdatedByName: requester.Name,
	})
	if err != nil {
		return nil, err
	}

	return s.toDTO(ctx, target.ID, saved)
}

// ResolveVisibility answers what an account may see in the catalog.
func (s *clientProductAccessService) ResolveVisibility(ctx context.Context, requesterRole, requesterID string) (domain.ProductVisibility, error) {
	// Staff browse the whole catalog, including drafts — they curate and explain it, so restricting
	// them would only hide products they are expected to discuss.
	if isAgencyStaff(requesterRole) {
		return domain.ProductVisibility{}, nil
	}

	userID, err := bson.ObjectIDFromHex(requesterID)
	if err != nil {
		// An unreadable caller ID is not a reason to widen access: nobody is identified, so nothing
		// is visible.
		return domain.ProductVisibility{Restricted: true, AllowedIDs: []bson.ObjectID{}}, nil
	}

	access, err := s.accessRepo.FindByUserID(ctx, userID)
	if err != nil {
		return domain.ProductVisibility{}, err
	}
	if access == nil || access.Mode != domain.ProductAccessSelected {
		return domain.ProductVisibility{}, nil
	}

	allowed := access.ProductIDs
	if allowed == nil {
		allowed = []bson.ObjectID{}
	}
	return domain.ProductVisibility{Restricted: true, AllowedIDs: allowed}, nil
}

// loadEditableClient resolves the target account and checks the caller may set its catalog.
//
// The same three rules as everywhere else in the agency model: staff only, the target must be a
// client (an admin's own catalog is not a thing), and a plain admin only reaches their own agency's
// clients. A target outside the caller's agency is reported as not found rather than forbidden, so
// the endpoint can't be used to discover which clients exist under other admins.
func (s *clientProductAccessService) loadEditableClient(ctx context.Context, requesterRole, requesterID, targetUserID string) (*domain.User, error) {
	if !isAgencyStaff(requesterRole) {
		return nil, fmt.Errorf("access denied: only agency staff can set what a client sees")
	}

	targetObjID, err := bson.ObjectIDFromHex(strings.TrimSpace(targetUserID))
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	target, err := s.userRepo.FindByID(ctx, targetObjID)
	if err != nil || target == nil {
		return nil, fmt.Errorf("client not found")
	}
	if target.Role != domain.RoleClient && target.Role != domain.RoleAdvisor {
		return nil, fmt.Errorf("only a client account has a product catalog")
	}

	if requesterRole == domain.RoleAdmin {
		callerAgency := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
		if !canAccessAgencyScopedRecord(requesterRole, callerAgency, target.AgencyID) {
			return nil, fmt.Errorf("client not found")
		}
	}

	return target, nil
}

// resolveProductIDs parses and verifies the submitted selection, dropping duplicates while keeping
// the admin's order. The whole selection is verified in one query rather than one per product.
func (s *clientProductAccessService) resolveProductIDs(ctx context.Context, ids []string) ([]bson.ObjectID, error) {
	if len(ids) > maxSelectedProducts {
		return nil, fmt.Errorf("too many products selected (maximum %d)", maxSelectedProducts)
	}

	parsed := make([]bson.ObjectID, 0, len(ids))
	seen := make(map[bson.ObjectID]bool, len(ids))

	for _, raw := range ids {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		objID, err := bson.ObjectIDFromHex(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid product ID: %s", trimmed)
		}
		if seen[objID] {
			continue
		}
		seen[objID] = true
		parsed = append(parsed, objID)
	}

	existing, err := s.existingSet(ctx, parsed)
	if err != nil {
		return nil, err
	}
	for _, objID := range parsed {
		if !existing[objID] {
			return nil, fmt.Errorf("product %s no longer exists — reload the catalog and try again", objID.Hex())
		}
	}

	return parsed, nil
}

// existingSet is the subset of ids still in the catalog, as a set for direct lookup.
func (s *clientProductAccessService) existingSet(ctx context.Context, ids []bson.ObjectID) (map[bson.ObjectID]bool, error) {
	if len(ids) == 0 {
		return map[bson.ObjectID]bool{}, nil
	}
	found, err := s.productRepo.FindExistingIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	set := make(map[bson.ObjectID]bool, len(found))
	for _, id := range found {
		set[id] = true
	}
	return set, nil
}

// toDTO shapes the record for the app, dropping any selected product that has since left the
// catalog so the count on screen matches what the client can actually open.
func (s *clientProductAccessService) toDTO(ctx context.Context, userID bson.ObjectID, access *domain.ClientProductAccess) (*domain.ClientProductAccessDTO, error) {
	dto := &domain.ClientProductAccessDTO{
		UserID:     userID.Hex(),
		Mode:       domain.ProductAccessAll,
		ProductIDs: []string{},
	}
	if access == nil {
		return dto, nil
	}

	dto.Configured = true
	dto.Mode = access.Mode
	dto.UpdatedByName = access.UpdatedByName
	if !access.UpdatedAt.IsZero() {
		updatedAt := access.UpdatedAt
		dto.UpdatedAt = &updatedAt
	}

	if access.Mode != domain.ProductAccessSelected {
		return dto, nil
	}

	existing, err := s.existingSet(ctx, access.ProductIDs)
	if err != nil {
		return nil, err
	}
	for _, productID := range access.ProductIDs {
		if !existing[productID] {
			continue
		}
		dto.ProductIDs = append(dto.ProductIDs, productID.Hex())
	}
	dto.SelectedCount = len(dto.ProductIDs)

	return dto, nil
}
