package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type familyMemberService struct {
	repo     domain.FamilyMemberRepository
	userRepo domain.UserRepository
}

// NewFamilyMemberService creates a new instance of FamilyMemberService.
func NewFamilyMemberService(repo domain.FamilyMemberRepository, userRepo domain.UserRepository) domain.FamilyMemberService {
	return &familyMemberService{
		repo:     repo,
		userRepo: userRepo,
	}
}

// AddMember creates a new family member record for the authenticated user.
func (s *familyMemberService) AddMember(ctx context.Context, userIDStr string, dto *domain.CreateFamilyMemberDTO) (*domain.FamilyMember, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	member := &domain.FamilyMember{
		UserID:          userID,
		Name:            dto.Name,
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

	return s.repo.Update(ctx, id, userID, dto)
}

// DeleteMember removes a family member record belonging to the authenticated user.
func (s *familyMemberService) DeleteMember(ctx context.Context, idStr, userIDStr string) error {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return fmt.Errorf("invalid family member ID format: %w", err)
	}

	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
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
