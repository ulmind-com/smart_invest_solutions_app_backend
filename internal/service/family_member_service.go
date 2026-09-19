package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type familyMemberService struct {
	repo       domain.FamilyMemberRepository
	userRepo   domain.UserRepository
	lifeRepo   domain.LifeInsuranceRepository
	healthRepo domain.HealthInsuranceRepository
	fdRepo     domain.FixedDepositRepository
}

// NewFamilyMemberService creates a new instance of FamilyMemberService.
func NewFamilyMemberService(
	repo domain.FamilyMemberRepository,
	userRepo domain.UserRepository,
	lifeRepo domain.LifeInsuranceRepository,
	healthRepo domain.HealthInsuranceRepository,
	fdRepo domain.FixedDepositRepository,
) domain.FamilyMemberService {
	return &familyMemberService{
		repo:       repo,
		userRepo:   userRepo,
		lifeRepo:   lifeRepo,
		healthRepo: healthRepo,
		fdRepo:     fdRepo,
	}
}

// AddMember creates a new family member record. A client always creates under their own account; an
// admin/super_admin may create on behalf of a client by passing dto.UserID, and a plain admin may
// only do so for a client inside their own agency (same resolveCallerAgencyID/
// canAccessAgencyScopedRecord pattern used by the on-behalf-of policy creation paths).
func (s *familyMemberService) AddMember(ctx context.Context, requesterRole, requesterID string, dto *domain.CreateFamilyMemberDTO) (*domain.FamilyMember, error) {
	targetIDStr := requesterID
	isStaff := requesterRole == domain.RoleAdmin || requesterRole == domain.RoleSuperAdmin
	if isStaff && strings.TrimSpace(dto.UserID) != "" {
		targetIDStr = strings.TrimSpace(dto.UserID)
	}

	userID, err := bson.ObjectIDFromHex(targetIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	if isStaff && targetIDStr != requesterID {
		targetUser, err := s.userRepo.FindByID(ctx, userID)
		if err != nil || targetUser == nil {
			return nil, fmt.Errorf("target user not found")
		}
		if requesterRole == domain.RoleAdmin {
			agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
			if !canAccessAgencyScopedRecord(requesterRole, agencyFilter, targetUser.AgencyID) {
				return nil, fmt.Errorf("target user not found")
			}
		}
	}

	name := strings.TrimSpace(dto.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	dob, err := normalizeISODate(dto.DateOfBirth, "date of birth")
	if err != nil {
		return nil, err
	}
	dto.DateOfBirth = dob

	member := &domain.FamilyMember{
		UserID:          userID,
		Name:            name,
		RelationWithHOF: dto.RelationWithHOF,
		Phone:           dto.Phone,
		Email:           dto.Email,
		BloodGroup:      dto.BloodGroup,
		DateOfBirth:     dto.DateOfBirth,
		LICCustomerID:   strings.TrimSpace(dto.LICCustomerID),
	}

	return s.repo.Create(ctx, member)
}

// GetMyMembers retrieves all family members belonging to the authenticated user.
func (s *familyMemberService) GetMyMembers(ctx context.Context, userIDStr string) (*domain.FamilyMemberListResponse, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	members, total, err := s.repo.FindAllByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &domain.FamilyMemberListResponse{
		Total: total,
		Data:  members,
	}, nil
}

// GetMemberByID retrieves a single family member by ID ensuring ownership check.
func (s *familyMemberService) GetMemberByID(ctx context.Context, idStr, userIDStr string) (*domain.FamilyMember, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid family member ID format: %w", err)
	}

	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	member, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if member.UserID != userID {
		return nil, fmt.Errorf("access denied: family member does not belong to you")
	}

	return member, nil
}

// UpdateMember updates a family member record belonging to the authenticated user.
func (s *familyMemberService) UpdateMember(ctx context.Context, idStr, userIDStr string, dto *domain.UpdateFamilyMemberDTO) (*domain.FamilyMember, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid family member ID format: %w", err)
	}

	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	if dto.LICCustomerID != nil {
		trimmed := strings.TrimSpace(*dto.LICCustomerID)
		dto.LICCustomerID = &trimmed
	}
	if dto.Name != nil {
		trimmed := strings.TrimSpace(*dto.Name)
		if trimmed == "" {
			return nil, fmt.Errorf("name cannot be empty")
		}
		dto.Name = &trimmed
	}
	if dto.DateOfBirth != nil {
		dob, err := normalizeISODate(*dto.DateOfBirth, "date of birth")
		if err != nil {
			return nil, err
		}
		dto.DateOfBirth = &dob
	}

	updated, err := s.repo.Update(ctx, id, userID, dto)
	if err != nil {
		return nil, err
	}

	// Policies cache the insured person's name; keep them in step with a rename so the policy
	// screens and PDF report don't keep showing the old name.
	if dto.Name != nil {
		if s.lifeRepo != nil {
			if err := s.lifeRepo.SyncInsuredName(ctx, id, updated.Name); err != nil {
				log.Error().Err(err).Str("family_member_id", idStr).Msg("failed to sync life insured name")
			}
		}
		if s.healthRepo != nil {
			if err := s.healthRepo.SyncInsuredName(ctx, id, updated.Name); err != nil {
				log.Error().Err(err).Str("family_member_id", idStr).Msg("failed to sync health insured name")
			}
		}
	}

	return updated, nil
}

func (s *familyMemberService) DeleteMember(ctx context.Context, idStr, userIDStr string) error {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return fmt.Errorf("invalid family member ID format: %w", err)
	}

	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}

	member, err := s.repo.FindByID(ctx, id)
	if err != nil || member == nil || member.UserID != userID {
		return fmt.Errorf("family member not found or access denied")
	}

	// Refuse to orphan records: every policy and deposit is filed against a person, and deleting
	// that person would leave them pointing at nobody (blank "insured" on screens and the report).
	linked := int64(0)
	for _, counter := range []interface {
		CountByFamilyMemberID(context.Context, bson.ObjectID) (int64, error)
	}{s.lifeRepo, s.healthRepo, s.fdRepo} {
		if counter == nil {
			continue
		}
		n, err := counter.CountByFamilyMemberID(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to check this member's policies: %w", err)
		}
		linked += n
	}
	if linked > 0 {
		return fmt.Errorf("%s still has %d linked polic(ies) or deposit(s) — move or delete those first", member.Name, linked)
	}

	return s.repo.Delete(ctx, id, userID)
}

// GetMembersByUserIDAdmin allows admin/super_admin to view family members of any user. A plain
// admin may only look up a client belonging to their own agency (same resolveCallerAgencyID/
// canAccessAgencyScopedRecord pattern used everywhere else).
func (s *familyMemberService) GetMembersByUserIDAdmin(ctx context.Context, requesterRole, requesterID, targetUserIDStr string) (*domain.FamilyMemberListResponse, error) {
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

	members, total, err := s.repo.FindAllByUserID(ctx, targetUserID)
	if err != nil {
		return nil, err
	}

	return &domain.FamilyMemberListResponse{
		Total: total,
		Data:  members,
	}, nil
}

// DeleteAllByUserID removes all family member records associated with a user ID string.
func (s *familyMemberService) DeleteAllByUserID(ctx context.Context, userIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}
	return s.repo.DeleteAllByUserID(ctx, userID)
}
