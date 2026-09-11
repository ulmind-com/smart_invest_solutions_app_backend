package service

import (
	"context"
	"fmt"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	DefaultAdvisorName    = "Samiran Samanta"
	DefaultAdvisorContact = "+91 9876543210"
)

type generalInsuranceService struct {
	repo     domain.GeneralInsuranceRepository
	userRepo domain.UserRepository
}

// NewGeneralInsuranceService creates a new instance of GeneralInsuranceService.
func NewGeneralInsuranceService(repo domain.GeneralInsuranceRepository, userRepo domain.UserRepository) domain.GeneralInsuranceService {
	return &generalInsuranceService{
		repo:     repo,
		userRepo: userRepo,
	}
}

// AddInsurance creates a new general insurance policy record.
func (s *generalInsuranceService) AddInsurance(ctx context.Context, userIDStr string, dto *domain.CreateGeneralInsuranceDTO) (*domain.GeneralInsurance, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	advisorName := dto.AdvisorName
	if advisorName == "" {
		advisorName = DefaultAdvisorName
	}

	advisorContact := dto.AdvisorContact
	if advisorContact == "" {
		advisorContact = DefaultAdvisorContact
	}

	policy := &domain.GeneralInsurance{
		UserID:         userID,
		VehicleNo:      dto.VehicleNo,
		PolicyNo:       dto.PolicyNo,
		DateOfExpiry:   dto.DateOfExpiry,
		CompanyName:    dto.CompanyName,
		AdvisorName:    advisorName,
		AdvisorContact: advisorContact,
	}

	return s.repo.Create(ctx, policy)
}

// GetMyInsurances retrieves all general insurance policies belonging to the authenticated user.
func (s *generalInsuranceService) GetMyInsurances(ctx context.Context, userIDStr string) (*domain.GeneralInsuranceListResponse, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	policies, total, err := s.repo.FindAllByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &domain.GeneralInsuranceListResponse{
		Total: total,
		Data:  policies,
	}, nil
}

// checkOwnership enforces that a client requester may only touch their own policies; super_admin
// bypasses this check completely, and a plain admin may only touch a policy whose owning client
// belongs to their own agency (same resolveCallerAgencyID/canAccessAgencyScopedRecord pattern used
// by Life/Health/FD) — a cross-agency policy reports "not found" rather than "forbidden".
func (s *generalInsuranceService) checkOwnership(ctx context.Context, requesterRole, requesterID string, ownerID bson.ObjectID) error {
	if requesterRole == domain.RoleSuperAdmin {
		return nil
	}
	if requesterRole == domain.RoleAdmin {
		owner, err := s.userRepo.FindByID(ctx, ownerID)
		if err != nil || owner == nil {
			return fmt.Errorf("policy not found")
		}
		agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
		if !canAccessAgencyScopedRecord(requesterRole, agencyFilter, owner.AgencyID) {
			return fmt.Errorf("policy not found")
		}
		return nil
	}
	requesterObjID, err := bson.ObjectIDFromHex(requesterID)
	if err != nil {
		return fmt.Errorf("invalid requester ID format: %w", err)
	}
	if ownerID != requesterObjID {
		return fmt.Errorf("access denied: policy does not belong to you")
	}
	return nil
}

// GetInsuranceByID retrieves a single policy by ID, enforcing ownership for client requesters and
// agency-scoping for a plain admin (super_admin may view any policy).
func (s *generalInsuranceService) GetInsuranceByID(ctx context.Context, requesterRole, requesterID, idStr string) (*domain.GeneralInsurance, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid policy ID format: %w", err)
	}

	policy, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.checkOwnership(ctx, requesterRole, requesterID, policy.UserID); err != nil {
		return nil, err
	}

	return policy, nil
}

// UpdateInsurance modifies an existing general insurance policy, enforcing ownership for client
// requesters and agency-scoping for a plain admin (super_admin may update any policy).
func (s *generalInsuranceService) UpdateInsurance(ctx context.Context, requesterRole, requesterID, idStr string, dto *domain.UpdateGeneralInsuranceDTO) (*domain.GeneralInsurance, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid policy ID format: %w", err)
	}

	existing, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.checkOwnership(ctx, requesterRole, requesterID, existing.UserID); err != nil {
		return nil, err
	}

	return s.repo.Update(ctx, id, existing.UserID, dto)
}

// DeleteInsurance removes a general insurance policy, enforcing ownership for client requesters
// and agency-scoping for a plain admin (super_admin may delete any policy).
func (s *generalInsuranceService) DeleteInsurance(ctx context.Context, requesterRole, requesterID, idStr string) error {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return fmt.Errorf("invalid policy ID format: %w", err)
	}

	existing, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.checkOwnership(ctx, requesterRole, requesterID, existing.UserID); err != nil {
		return err
	}

	return s.repo.Delete(ctx, id, existing.UserID)
}

// GetInsurancesByUserIDAdmin allows admin/super_admin to view general insurance policies of any
// user. A plain admin may only look up a client belonging to their own agency (same
// resolveCallerAgencyID/canAccessAgencyScopedRecord pattern used everywhere else).
func (s *generalInsuranceService) GetInsurancesByUserIDAdmin(ctx context.Context, requesterRole, requesterID, targetUserIDStr string) (*domain.GeneralInsuranceListResponse, error) {
	targetUserID, err := bson.ObjectIDFromHex(targetUserIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid target user ID format: %w", err)
	}

	if requesterRole == domain.RoleAdmin {
		targetUser, err := s.userRepo.FindByID(ctx, targetUserID)
		if err != nil || targetUser == nil {
			return nil, fmt.Errorf("user not found")
		}
		agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
		if !canAccessAgencyScopedRecord(requesterRole, agencyFilter, targetUser.AgencyID) {
			return nil, fmt.Errorf("user not found")
		}
	}

	policies, total, err := s.repo.FindAllByUserID(ctx, targetUserID)
	if err != nil {
		return nil, err
	}

	return &domain.GeneralInsuranceListResponse{
		Total: total,
		Data:  policies,
	}, nil
}

// DeleteAllByUserID removes all general insurance policies associated with a user ID string.
func (s *generalInsuranceService) DeleteAllByUserID(ctx context.Context, userIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}
	return s.repo.DeleteAllByUserID(ctx, userID)
}

// GetAllInsurancesAdmin returns a paginated master list of every general insurance policy across
// all clients, each enriched with the owning customer's name and contact number — lets Admin/
// Super Admin see at a glance which client holds which policy, vehicle, expiry date, and insurer.
// A plain admin only ever sees policies belonging to their own agency's clients; super_admin sees
// every agency (see resolveCallerAgencyID / canAccessAgencyScopedRecord in user_service.go).
func (s *generalInsuranceService) GetAllInsurancesAdmin(ctx context.Context, requesterRole, requesterID string, page, limit int64) ([]*domain.GeneralInsuranceWithCustomer, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
	if requesterRole == domain.RoleAdmin && agencyFilter == "" {
		// Fail closed, exactly like the other agency-scoped listings: an admin whose own agency
		// can't be resolved must never fall through to the platform-wide (super_admin) view.
		return []*domain.GeneralInsuranceWithCustomer{}, 0, nil
	}

	return s.repo.FindAllAdmin(ctx, page, limit, agencyFilter)
}
