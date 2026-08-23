package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type lifeInsuranceService struct {
	repo             domain.LifeInsuranceRepository
	userRepo         domain.UserRepository
	familyMemberRepo domain.FamilyMemberRepository
}

// NewLifeInsuranceService creates a new instance of LifeInsuranceService.
func NewLifeInsuranceService(repo domain.LifeInsuranceRepository, userRepo domain.UserRepository, familyMemberRepo domain.FamilyMemberRepository) domain.LifeInsuranceService {
	return &lifeInsuranceService{
		repo:             repo,
		userRepo:         userRepo,
		familyMemberRepo: familyMemberRepo,
	}
}

// resolveTargetUserID determines which client account a policy belongs to: a client always acts
// on their own account; admin/super_admin may target any client via dto.UserID ("Select Member"
// flow on the admin side starts from picking a client first).
func (s *lifeInsuranceService) resolveTargetUserID(requesterRole, requesterID, dtoUserID string) (bson.ObjectID, error) {
	targetIDStr := requesterID
	if (requesterRole == domain.RoleAdmin || requesterRole == domain.RoleSuperAdmin) && dtoUserID != "" {
		targetIDStr = dtoUserID
	}
	return bson.ObjectIDFromHex(targetIDStr)
}

// checkOwnership enforces that a client requester may only touch their own policies;
// admin/super_admin bypass this check completely.
func (s *lifeInsuranceService) checkOwnership(requesterRole, requesterID string, ownerID bson.ObjectID) error {
	if requesterRole == domain.RoleAdmin || requesterRole == domain.RoleSuperAdmin {
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

// CreatePolicy adds a new life insurance policy after verifying the target client exists, the
// family member actually belongs to that client (prevents ID spoofing), and the policy dates are
// chronologically sound (DOC before MaturityDate).
func (s *lifeInsuranceService) CreatePolicy(ctx context.Context, requesterRole, requesterID string, dto *domain.CreateLifeInsuranceDTO) (*domain.LifeInsurance, error) {
	userID, err := s.resolveTargetUserID(requesterRole, requesterID, dto.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	targetUser, err := s.userRepo.FindByID(ctx, userID)
	if err != nil || targetUser == nil {
		return nil, fmt.Errorf("target user not found")
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

	if !dto.PolicyDetails.DOC.Before(dto.PolicyDetails.MaturityDate) {
		return nil, fmt.Errorf("date of commencement must be before the maturity date")
	}

	policy := &domain.LifeInsurance{
		UserID:         userID,
		FamilyMemberID: familyMemberID,
		CompanyName:    dto.CompanyName,
		PolicyDetails: domain.PolicyDetails{
			PolicyNo:        dto.PolicyDetails.PolicyNo,
			PlanName:        dto.PolicyDetails.PlanName,
			LifeInsuredName: familyMember.Name, // derived from the family member, cached for record-keeping
			NomineeName:     dto.PolicyDetails.NomineeName,
			SumAssured:      dto.PolicyDetails.SumAssured,
			Term:            dto.PolicyDetails.Term,
			PPT:             dto.PolicyDetails.PPT,
			DOC:             dto.PolicyDetails.DOC,
			MaturityDate:    dto.PolicyDetails.MaturityDate,
		},
		PremiumDetails: domain.PremiumDetails{
			InstallmentPremium: dto.PremiumDetails.InstallmentPremium,
			NextDueDate:        dto.PremiumDetails.NextDueDate,
			PaymentMode:        dto.PremiumDetails.PaymentMode,
		},
		IsMapped: dto.IsMapped,
	}

	return s.repo.Create(ctx, policy)
}

// GetPolicyByID retrieves a single policy, enforcing that a client requester owns it.
func (s *lifeInsuranceService) GetPolicyByID(ctx context.Context, requesterRole, requesterID, idStr string) (*domain.LifeInsurance, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid policy ID format: %w", err)
	}

	policy, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.checkOwnership(requesterRole, requesterID, policy.UserID); err != nil {
		return nil, err
	}

	return policy, nil
}

// GetMyPolicies retrieves all policies belonging to the authenticated client.
func (s *lifeInsuranceService) GetMyPolicies(ctx context.Context, requesterID string) (*domain.LifeInsuranceListResponse, error) {
	userID, err := bson.ObjectIDFromHex(requesterID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	policies, total, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &domain.LifeInsuranceListResponse{Total: total, Data: policies}, nil
}

// GetAllPolicies returns the paginated Admin master list across every client, optionally
// filtered to unmapped/mapped policies.
func (s *lifeInsuranceService) GetAllPolicies(ctx context.Context, page, limit int64, isMapped *bool) ([]*domain.LifeInsuranceWithCustomer, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	return s.repo.GetAll(ctx, page, limit, isMapped)
}

// UpdatePolicy modifies an existing policy, enforcing ownership for client requesters,
// re-verifying any reassigned family member belongs to the policy's owner (and refreshing the
// cached LifeInsuredName to match), and re-validating DOC/MaturityDate chronology.
func (s *lifeInsuranceService) UpdatePolicy(ctx context.Context, requesterRole, requesterID, idStr string, dto *domain.UpdateLifeInsuranceDTO) (*domain.LifeInsurance, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid policy ID format: %w", err)
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.checkOwnership(requesterRole, requesterID, existing.UserID); err != nil {
		return nil, err
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
		dto.LifeInsuredName = &familyMember.Name
	}

	doc := existing.PolicyDetails.DOC
	if dto.DOC != nil {
		doc = *dto.DOC
	}
	maturityDate := existing.PolicyDetails.MaturityDate
	if dto.MaturityDate != nil {
		maturityDate = *dto.MaturityDate
	}
	if !doc.Before(maturityDate) {
		return nil, fmt.Errorf("date of commencement must be before the maturity date")
	}

	return s.repo.Update(ctx, id, dto)
}

// DeletePolicy removes a policy, enforcing ownership for client requesters.
func (s *lifeInsuranceService) DeletePolicy(ctx context.Context, requesterRole, requesterID, idStr string) error {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return fmt.Errorf("invalid policy ID format: %w", err)
	}

	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.checkOwnership(requesterRole, requesterID, existing.UserID); err != nil {
		return err
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		log.Warn().Err(err).Str("policy_id", idStr).Msg("failed to delete life insurance policy")
		return err
	}

	return nil
}

// DeleteAllByUserID removes all life insurance policies associated with a user ID string —
// wired into the account-deletion cascade alongside documents/family members/general insurance.
func (s *lifeInsuranceService) DeleteAllByUserID(ctx context.Context, userIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}
	return s.repo.DeleteAllByUserID(ctx, userID)
}
