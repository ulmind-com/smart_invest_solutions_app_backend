package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type healthInsuranceService struct {
	repo             domain.HealthInsuranceRepository
	userRepo         domain.UserRepository
	familyMemberRepo domain.FamilyMemberRepository
}

// NewHealthInsuranceService creates a new instance of HealthInsuranceService.
func NewHealthInsuranceService(repo domain.HealthInsuranceRepository, userRepo domain.UserRepository, familyMemberRepo domain.FamilyMemberRepository) domain.HealthInsuranceService {
	return &healthInsuranceService{
		repo:             repo,
		userRepo:         userRepo,
		familyMemberRepo: familyMemberRepo,
	}
}

// resolveTargetUserID determines which client account a policy belongs to: a client always acts
// on their own account; admin/super_admin may target any client via dto.UserID.
func (s *healthInsuranceService) resolveTargetUserID(requesterRole, requesterID, dtoUserID string) (bson.ObjectID, error) {
	targetIDStr := requesterID
	if (requesterRole == domain.RoleAdmin || requesterRole == domain.RoleSuperAdmin) && dtoUserID != "" {
		targetIDStr = dtoUserID
	}
	return bson.ObjectIDFromHex(targetIDStr)
}

// checkOwnership enforces that a client requester may only touch their own policies; super_admin
// bypasses this check completely, and a plain admin may only touch a policy whose owning client
// belongs to their own agency (same resolveCallerAgencyID/canAccessAgencyScopedRecord pattern used
// for the admin master list) — a cross-agency policy reports "not found" rather than "forbidden".
func (s *healthInsuranceService) checkOwnership(ctx context.Context, requesterRole, requesterID string, ownerID bson.ObjectID) error {
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

// CreatePolicy adds a new health insurance policy after verifying the target client exists, the
// family member (primary insured) actually belongs to that client (prevents ID spoofing), and
// the dates are chronologically sound (DOC before ExpiryDate).
func (s *healthInsuranceService) CreatePolicy(ctx context.Context, requesterRole, requesterID string, dto *domain.CreateHealthInsuranceDTO) (*domain.HealthInsurance, error) {
	userID, err := s.resolveTargetUserID(requesterRole, requesterID, dto.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	targetUser, err := s.userRepo.FindByID(ctx, userID)
	if err != nil || targetUser == nil {
		return nil, fmt.Errorf("target user not found")
	}

	// A plain admin may only create a policy on behalf of a client under their own agency.
	if requesterRole == domain.RoleAdmin {
		agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
		if !canAccessAgencyScopedRecord(requesterRole, agencyFilter, targetUser.AgencyID) {
			return nil, fmt.Errorf("target user not found")
		}
	}

	familyMemberID, err := bson.ObjectIDFromHex(dto.FamilyMemberID)
	if err != nil {
		return nil, fmt.Errorf("invalid family member ID format: %w", err)
	}

	familyMember, err := s.familyMemberRepo.FindByID(ctx, familyMemberID)
	if err != nil || familyMember == nil {
		return nil, fmt.Errorf("family member not found")
	}
	if familyMember.UserID != userID {
		return nil, fmt.Errorf("family member does not belong to the specified client")
	}

	if !dto.PolicyDetails.DOC.Before(dto.PolicyDetails.ExpiryDate) {
		return nil, fmt.Errorf("date of commencement must be before the expiry date")
	}

	policy := &domain.HealthInsurance{
		UserID:         userID,
		FamilyMemberID: familyMemberID,
		CompanyName:    dto.CompanyName,
		PolicyDetails: domain.HealthPolicyDetails{
			PolicyNo:           dto.PolicyDetails.PolicyNo,
			PlanName:           dto.PolicyDetails.PlanName,
			PrimaryInsuredName: familyMember.Name, // derived from the family member, cached for record-keeping
			SumInsured:         dto.PolicyDetails.SumInsured,
			DOC:                dto.PolicyDetails.DOC,
			ExpiryDate:         dto.PolicyDetails.ExpiryDate,
		},
		PremiumDetails: domain.HealthPremiumDetails{
			InstallmentPremium: dto.PremiumDetails.InstallmentPremium,
			NextDueDate:        dto.PremiumDetails.NextDueDate,
			PaymentMode:        dto.PremiumDetails.PaymentMode,
		},
		IsMapped: dto.IsMapped,
	}

	return s.repo.Create(ctx, policy)
}

// GetPolicyByID retrieves a single policy, enforcing that a client requester owns it.
func (s *healthInsuranceService) GetPolicyByID(ctx context.Context, requesterRole, requesterID, idStr string) (*domain.HealthInsurance, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid policy ID format: %w", err)
	}

	policy, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.checkOwnership(ctx, requesterRole, requesterID, policy.UserID); err != nil {
		return nil, err
	}

	return policy, nil
}

// GetMyPolicies retrieves all policies belonging to the authenticated client.
func (s *healthInsuranceService) GetMyPolicies(ctx context.Context, requesterID string) (*domain.HealthInsuranceListResponse, error) {
	userID, err := bson.ObjectIDFromHex(requesterID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	policies, total, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &domain.HealthInsuranceListResponse{Total: total, Data: policies}, nil
}

// GetPoliciesByUserIDAdmin lets admin/super_admin view a specific client's full, unpaginated
// policy list. A plain admin may only look up a client belonging to their own agency (same
// resolveCallerAgencyID/canAccessAgencyScopedRecord pattern used everywhere else).
func (s *healthInsuranceService) GetPoliciesByUserIDAdmin(ctx context.Context, requesterRole, requesterID, targetUserIDStr string) (*domain.HealthInsuranceListResponse, error) {
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

	policies, total, err := s.repo.GetByUserID(ctx, targetUserID)
	if err != nil {
		return nil, err
	}

	return &domain.HealthInsuranceListResponse{Total: total, Data: policies}, nil
}

// GetAllPolicies returns the paginated Admin master list across every client, optionally
// filtered to unmapped/mapped policies. A plain admin only ever sees policies belonging to their
// own agency's clients; super_admin sees every agency (see resolveCallerAgencyID /
// canAccessAgencyScopedRecord in user_service.go).
func (s *healthInsuranceService) GetAllPolicies(ctx context.Context, requesterRole, requesterID string, page, limit int64, isMapped *bool, licCustomerID string) ([]*domain.HealthInsuranceWithCustomer, int64, error) {
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
		return []*domain.HealthInsuranceWithCustomer{}, 0, nil
	}

	return s.repo.GetAll(ctx, page, limit, isMapped, strings.TrimSpace(licCustomerID), agencyFilter)
}

// UpdatePolicy modifies an existing policy, enforcing ownership for client requesters,
// re-verifying any reassigned family member belongs to the policy's owner (refreshing the cached
// PrimaryInsuredName to match), and re-validating DOC/ExpiryDate chronology.
//
// Security patch: IsMapped is an admin-tracking flag. If the requester is a client, whatever
// they sent in IsMapped is discarded (set back to nil) BEFORE hitting the repository, so the
// partial $set update in the repository never touches that field — the existing DB value is
// silently preserved. Only admin/super_admin may actually change it.
func (s *healthInsuranceService) UpdatePolicy(ctx context.Context, requesterRole, requesterID, idStr string, dto *domain.UpdateHealthInsuranceDTO) (*domain.HealthInsurance, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid policy ID format: %w", err)
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.checkOwnership(ctx, requesterRole, requesterID, existing.UserID); err != nil {
		return nil, err
	}

	if requesterRole != domain.RoleAdmin && requesterRole != domain.RoleSuperAdmin {
		dto.IsMapped = nil
	}

	if dto.FamilyMemberID != nil {
		familyMemberID, err := bson.ObjectIDFromHex(*dto.FamilyMemberID)
		if err != nil {
			return nil, fmt.Errorf("invalid family member ID format: %w", err)
		}
		familyMember, err := s.familyMemberRepo.FindByID(ctx, familyMemberID)
		if err != nil || familyMember == nil {
			return nil, fmt.Errorf("family member not found")
		}
		if familyMember.UserID != existing.UserID {
			return nil, fmt.Errorf("family member does not belong to the policy owner")
		}
		dto.PrimaryInsuredName = &familyMember.Name
	}

	doc := existing.PolicyDetails.DOC
	if dto.DOC != nil {
		doc = *dto.DOC
	}
	expiryDate := existing.PolicyDetails.ExpiryDate
	if dto.ExpiryDate != nil {
		expiryDate = *dto.ExpiryDate
	}
	if !doc.Before(expiryDate) {
		return nil, fmt.Errorf("date of commencement must be before the expiry date")
	}

	return s.repo.Update(ctx, id, dto)
}

// DeletePolicy removes a policy, enforcing ownership for client requesters.
func (s *healthInsuranceService) DeletePolicy(ctx context.Context, requesterRole, requesterID, idStr string) error {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return fmt.Errorf("invalid policy ID format: %w", err)
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.checkOwnership(ctx, requesterRole, requesterID, existing.UserID); err != nil {
		return err
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		log.Warn().Err(err).Str("policy_id", idStr).Msg("failed to delete health insurance policy")
		return err
	}

	return nil
}

// DeleteAllByUserID removes all health insurance policies associated with a user ID string —
// wired into the account-deletion cascade alongside documents/family members/general & life
// insurance/fixed deposits.
func (s *healthInsuranceService) DeleteAllByUserID(ctx context.Context, userIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}
	return s.repo.DeleteAllByUserID(ctx, userID)
}
