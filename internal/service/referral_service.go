package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// maxAdminsPerSummaryPage bounds the staff roster walked to build the referral leaderboard.
const maxAdminsPerSummaryPage = 100

// referralService implements domain.ReferralService.
//
// Referrals are an agency-staff feature: an admin shares their referral code, and every client who
// signs up with it is attributed to them. Clients have no referral code of their own, and a
// referral carries no reward — it exists so a super admin can see which admin is bringing in
// business, and how much.
type referralService struct {
	referralRepo domain.ReferralRepository
	userRepo     domain.UserRepository
}

// NewReferralService creates a new referral service.
func NewReferralService(referralRepo domain.ReferralRepository, userRepo domain.UserRepository) domain.ReferralService {
	return &referralService{
		referralRepo: referralRepo,
		userRepo:     userRepo,
	}
}

// GetMyStats returns the calling staff member's own referral code and how their referrals are doing.
func (s *referralService) GetMyStats(ctx context.Context, requesterRole, requesterID string) (*domain.ReferralStatsDTO, error) {
	if !isAgencyStaff(requesterRole) {
		return nil, fmt.Errorf("referral codes belong to agency staff — clients don't refer accounts")
	}

	objectID, err := bson.ObjectIDFromHex(requesterID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	user, err := s.userRepo.FindByID(ctx, objectID)
	if err != nil || user == nil {
		return nil, fmt.Errorf("account not found")
	}

	counts, err := s.referralRepo.CountsByReferrerID(ctx, objectID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch referral statistics: %w", err)
	}

	return &domain.ReferralStatsDTO{
		ReferralCode:   user.ReferralCode,
		TotalPending:   counts.Pending,
		TotalCompleted: counts.Completed,
	}, nil
}

// GetAllReferrals lists the referral ledger. A plain admin is pinned to their own referrals; a
// super admin sees everything, and may narrow the list to one admin via referrerIDFilter.
func (s *referralService) GetAllReferrals(ctx context.Context, requesterRole, requesterID, referrerIDFilter string, page, limit int64) (*domain.ReferralListResponse, error) {
	if !isAgencyStaff(requesterRole) {
		return nil, fmt.Errorf("only agency staff can view the referral ledger")
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	var filter *bson.ObjectID
	if requesterRole == domain.RoleSuperAdmin {
		if trimmed := strings.TrimSpace(referrerIDFilter); trimmed != "" {
			objID, err := bson.ObjectIDFromHex(trimmed)
			if err != nil {
				return nil, fmt.Errorf("invalid referrer ID format")
			}
			filter = &objID
		}
	} else {
		// A plain admin's ledger is their own work only — never another admin's clients.
		objID, err := bson.ObjectIDFromHex(requesterID)
		if err != nil {
			return nil, fmt.Errorf("invalid user ID format: %w", err)
		}
		filter = &objID
	}

	records, total, err := s.referralRepo.GetAll(ctx, page, limit, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch referral records: %w", err)
	}

	return &domain.ReferralListResponse{
		Total: total,
		Data:  records,
	}, nil
}

// GetAdminSummary builds the per-admin leaderboard: every staff account with the number of clients
// it has referred. Admins with no referrals are included (with zeros) so the list doubles as a
// roster, and it is sorted by conversions, highest first.
func (s *referralService) GetAdminSummary(ctx context.Context, requesterRole string) ([]*domain.AdminReferralSummary, error) {
	if requesterRole != domain.RoleSuperAdmin {
		return nil, fmt.Errorf("only a super admin can see the per-admin referral summary")
	}

	counts, err := s.referralRepo.CountsByReferrer(ctx)
	if err != nil {
		return nil, err
	}

	summaries := make([]*domain.AdminReferralSummary, 0, len(counts))
	seen := map[bson.ObjectID]bool{}

	for page := int64(1); ; page++ {
		staff, total, err := s.userRepo.FindAllByRoles(ctx, []string{domain.RoleAdmin, domain.RoleSuperAdmin}, page, maxAdminsPerSummaryPage)
		if err != nil {
			return nil, fmt.Errorf("failed to load staff accounts: %w", err)
		}
		for _, member := range staff {
			seen[member.ID] = true
			entry := counts[member.ID]
			summaries = append(summaries, &domain.AdminReferralSummary{
				AdminUserID:    member.ID,
				AdminID:        member.AdminID,
				Name:           member.Name,
				Email:          member.Email,
				ReferralCode:   member.ReferralCode,
				Role:           member.Role,
				TotalPending:   entry.Pending,
				TotalCompleted: entry.Completed,
				Total:          entry.Pending + entry.Completed,
			})
		}
		if len(staff) == 0 || page*maxAdminsPerSummaryPage >= total {
			break
		}
	}

	// Referrals made before this became a staff-only feature (or by an account since deleted) still
	// belong in the totals, so they are listed too rather than silently disappearing.
	for referrerID, entry := range counts {
		if seen[referrerID] {
			continue
		}
		summary := &domain.AdminReferralSummary{
			AdminUserID:    referrerID,
			Name:           "Former referrer",
			TotalPending:   entry.Pending,
			TotalCompleted: entry.Completed,
			Total:          entry.Pending + entry.Completed,
		}
		if referrer, err := s.userRepo.FindByID(ctx, referrerID); err == nil && referrer != nil {
			summary.Name = referrer.Name
			summary.Email = referrer.Email
			summary.Role = referrer.Role
			summary.AdminID = referrer.AdminID
			summary.ReferralCode = referrer.ReferralCode
		}
		summaries = append(summaries, summary)
	}

	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].TotalCompleted != summaries[j].TotalCompleted {
			return summaries[i].TotalCompleted > summaries[j].TotalCompleted
		}
		if summaries[i].Total != summaries[j].Total {
			return summaries[i].Total > summaries[j].Total
		}
		return strings.ToLower(summaries[i].Name) < strings.ToLower(summaries[j].Name)
	})

	return summaries, nil
}
