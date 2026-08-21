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
	repo domain.GeneralInsuranceRepository
}

// NewGeneralInsuranceService creates a new instance of GeneralInsuranceService.
func NewGeneralInsuranceService(repo domain.GeneralInsuranceRepository) domain.GeneralInsuranceService {
	return &generalInsuranceService{
		repo: repo,
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

// GetInsuranceByID retrieves a single policy by ID with ownership verification.
func (s *generalInsuranceService) GetInsuranceByID(ctx context.Context, idStr, userIDStr string) (*domain.GeneralInsurance, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid policy ID format: %w", err)
	}

	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	policy, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if policy.UserID != userID {
		return nil, fmt.Errorf("access denied: policy does not belong to you")
	}

	return policy, nil
}

// UpdateInsurance modifies an existing general insurance policy.
func (s *generalInsuranceService) UpdateInsurance(ctx context.Context, idStr, userIDStr string, dto *domain.UpdateGeneralInsuranceDTO) (*domain.GeneralInsurance, error) {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid policy ID format: %w", err)
	}

	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	return s.repo.Update(ctx, id, userID, dto)
}

// DeleteInsurance removes a general insurance policy.
func (s *generalInsuranceService) DeleteInsurance(ctx context.Context, idStr, userIDStr string) error {
	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return fmt.Errorf("invalid policy ID format: %w", err)
	}

	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}

	return s.repo.Delete(ctx, id, userID)
}

// GetInsurancesByUserIDAdmin allows admins to view general insurance policies of any user.
func (s *generalInsuranceService) GetInsurancesByUserIDAdmin(ctx context.Context, targetUserIDStr string) (*domain.GeneralInsuranceListResponse, error) {
	targetUserID, err := bson.ObjectIDFromHex(targetUserIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid target user ID format: %w", err)
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
