package service

import (
	"context"
	"fmt"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type referralService struct {
	referralRepo domain.ReferralRepository
	userRepo     domain.UserRepository
}

// NewReferralService initializes a new ReferralService.
func NewReferralService(referralRepo domain.ReferralRepository, userRepo domain.UserRepository) domain.ReferralService {
	return &referralService{
		referralRepo: referralRepo,
		userRepo:     userRepo,
	}
}

// GetMyStats retrieves referral metrics and validity details for the logged-in client.
func (s *referralService) GetMyStats(ctx context.Context, userIDStr string) (*domain.ReferralStatsDTO, error) {
	objectID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	user, err := s.userRepo.FindByID(ctx, objectID)
	if err != nil || user == nil {
		return nil, fmt.Errorf("user account not found")
	}

	pending, completed, daysEarned, err := s.referralRepo.GetStatsByReferrerID(ctx, objectID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch referral statistics: %w", err)
	}

	return &domain.ReferralStatsDTO{
		ReferralCode:       user.ReferralCode,
		AppValidityEndDate: user.AppValidityEndDate,
		TotalPending:       pending,
		TotalCompleted:     completed,
		TotalDaysEarned:    daysEarned,
	}, nil
}

// GetAllReferrals retrieves a paginated master list of all referral records across the agency (Admin view).
func (s *referralService) GetAllReferrals(ctx context.Context, requesterRole, requesterID string, page, limit int64) (*domain.ReferralListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	// A plain admin only ever sees referrals made by clients of their own agency — the ledger
	// carries other agencies' prospects' email addresses.
	agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
	if requesterRole == domain.RoleAdmin && agencyFilter == "" {
		return &domain.ReferralListResponse{Total: 0, Data: []*domain.ReferralRecordWithDetails{}}, nil
	}

	records, total, err := s.referralRepo.GetAll(ctx, page, limit, agencyFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch referral records: %w", err)
	}

	return &domain.ReferralListResponse{
		Total: total,
		Data:  records,
	}, nil
}
