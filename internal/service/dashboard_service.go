package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/sync/errgroup"
)

// maxClientRosterFetch bounds the in-memory client fetch used to count active clients. None of
// the existing repositories expose a dedicated "count by role + active status" query, and this
// module intentionally introduces no new repository code — so the full client roster is fetched
// once and filtered in memory. Revisit with a dedicated count query if the client base grows large
// enough for this to matter.
const maxClientRosterFetch = 1_000_000

// dashboardService is a pure orchestrator: it owns no collection of its own and holds no state
// beyond references to the repositories it aggregates data from.
type dashboardService struct {
	userRepo             domain.UserRepository
	familyMemberRepo     domain.FamilyMemberRepository
	lifeInsuranceRepo    domain.LifeInsuranceRepository
	healthInsuranceRepo  domain.HealthInsuranceRepository
	generalInsuranceRepo domain.GeneralInsuranceRepository
	fixedDepositRepo     domain.FixedDepositRepository
	accessRequestRepo    domain.AccessRequestRepository
}

// NewDashboardService creates a new instance of DashboardService. AccessRequestRepository is
// required in addition to the six financial/user repositories because PendingAccessRequests
// (part of AdminDashboardDTO) has no other data source.
func NewDashboardService(
	userRepo domain.UserRepository,
	familyMemberRepo domain.FamilyMemberRepository,
	lifeInsuranceRepo domain.LifeInsuranceRepository,
	healthInsuranceRepo domain.HealthInsuranceRepository,
	generalInsuranceRepo domain.GeneralInsuranceRepository,
	fixedDepositRepo domain.FixedDepositRepository,
	accessRequestRepo domain.AccessRequestRepository,
) domain.DashboardService {
	return &dashboardService{
		userRepo:             userRepo,
		familyMemberRepo:     familyMemberRepo,
		lifeInsuranceRepo:    lifeInsuranceRepo,
		healthInsuranceRepo:  healthInsuranceRepo,
		generalInsuranceRepo: generalInsuranceRepo,
		fixedDepositRepo:     fixedDepositRepo,
		accessRequestRepo:    accessRequestRepo,
	}
}

// GetClientDashboard aggregates a single client's totals across every financial module plus a
// chronologically-sorted list of Life/Health premiums due within the next 30 days. Each count
// query runs concurrently via errgroup since they are independent and touch different collections.
func (s *dashboardService) GetClientDashboard(ctx context.Context, userIDStr string) (*domain.ClientDashboardDTO, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	var (
		familyTotal, lifeTotal, healthTotal, generalTotal, fdTotal int64
		lifePolicies                                               []*domain.LifeInsurance
		healthPolicies                                             []*domain.HealthInsurance
	)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		_, total, err := s.familyMemberRepo.FindAllByUserID(gctx, userID)
		familyTotal = total
		return err
	})
	g.Go(func() error {
		policies, total, err := s.lifeInsuranceRepo.GetByUserID(gctx, userID)
		lifePolicies, lifeTotal = policies, total
		return err
	})
	g.Go(func() error {
		policies, total, err := s.healthInsuranceRepo.GetByUserID(gctx, userID)
		healthPolicies, healthTotal = policies, total
		return err
	})
	g.Go(func() error {
		_, total, err := s.generalInsuranceRepo.FindAllByUserID(gctx, userID)
		generalTotal = total
		return err
	})
	g.Go(func() error {
		_, total, err := s.fixedDepositRepo.GetByUserID(gctx, userID)
		fdTotal = total
		return err
	})

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("failed to load client dashboard data: %w", err)
	}

	now := time.Now().UTC()
	windowEnd := now.AddDate(0, 1, 0)

	upcoming := make([]domain.UpcomingPayment, 0)
	for _, p := range lifePolicies {
		due := p.PremiumDetails.NextDueDate
		if !due.Before(now) && due.Before(windowEnd) {
			upcoming = append(upcoming, domain.UpcomingPayment{
				Type:       "Life Insurance",
				EntityName: p.PolicyDetails.PlanName,
				Amount:     p.PremiumDetails.InstallmentPremium,
				DueDate:    due,
			})
		}
	}
	for _, p := range healthPolicies {
		due := p.PremiumDetails.NextDueDate
		if !due.Before(now) && due.Before(windowEnd) {
			upcoming = append(upcoming, domain.UpcomingPayment{
				Type:       "Health Insurance",
				EntityName: p.PolicyDetails.PlanName,
				Amount:     p.PremiumDetails.InstallmentPremium,
				DueDate:    due,
			})
		}
	}

	sort.Slice(upcoming, func(i, j int) bool {
		return upcoming[i].DueDate.Before(upcoming[j].DueDate)
	})

	return &domain.ClientDashboardDTO{
		TotalFamilyMembers:   familyTotal,
		TotalLifePolicies:    lifeTotal,
		TotalHealthPolicies:  healthTotal,
		TotalGeneralPolicies: generalTotal,
		TotalFixedDeposits:   fdTotal,
		UpcomingPremiums:     upcoming,
	}, nil
}

// GetAdminDashboard aggregates platform-wide totals: active clients, pending access requests, and
// mapped/unmapped policy counts across Life, Health, General Insurance, and Fixed Deposits.
//
// Note: General Insurance has no is_mapped field in its data model (unlike Life/Health/FD), so its
// policies can't be individually classified as mapped or unmapped. They are conservatively counted
// toward Unmapped so they still contribute to the overall total instead of silently disappearing
// from PolicyStats — worth revisiting if is_mapped tracking is ever added to General Insurance.
func (s *dashboardService) GetAdminDashboard(ctx context.Context) (*domain.AdminDashboardDTO, error) {
	var (
		activeClients                int64
		pendingRequests              int64
		lifeMapped, lifeUnmapped     int64
		healthMapped, healthUnmapped int64
		generalTotal                 int64
		fdMapped, fdUnmapped         int64
	)

	mappedFilter, unmappedFilter := true, false

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		clients, _, err := s.userRepo.FindAllByRoles(gctx, []string{domain.RoleClient}, 1, maxClientRosterFetch)
		if err != nil {
			return err
		}
		for _, c := range clients {
			if c.IsActive {
				activeClients++
			}
		}
		return nil
	})
	g.Go(func() error {
		_, total, err := s.accessRequestRepo.FindAll(gctx, domain.AccessStatusPending, 1, 1)
		pendingRequests = total
		return err
	})
	g.Go(func() error {
		_, total, err := s.lifeInsuranceRepo.GetAll(gctx, 1, 1, &mappedFilter)
		lifeMapped = total
		return err
	})
	g.Go(func() error {
		_, total, err := s.lifeInsuranceRepo.GetAll(gctx, 1, 1, &unmappedFilter)
		lifeUnmapped = total
		return err
	})
	g.Go(func() error {
		_, total, err := s.healthInsuranceRepo.GetAll(gctx, 1, 1, &mappedFilter)
		healthMapped = total
		return err
	})
	g.Go(func() error {
		_, total, err := s.healthInsuranceRepo.GetAll(gctx, 1, 1, &unmappedFilter)
		healthUnmapped = total
		return err
	})
	g.Go(func() error {
		_, total, err := s.generalInsuranceRepo.FindAllAdmin(gctx, 1, 1)
		generalTotal = total
		return err
	})
	g.Go(func() error {
		_, total, err := s.fixedDepositRepo.GetAll(gctx, 1, 1, &mappedFilter)
		fdMapped = total
		return err
	})
	g.Go(func() error {
		_, total, err := s.fixedDepositRepo.GetAll(gctx, 1, 1, &unmappedFilter)
		fdUnmapped = total
		return err
	})

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("failed to load admin dashboard data: %w", err)
	}

	return &domain.AdminDashboardDTO{
		TotalActiveClients:    activeClients,
		PendingAccessRequests: pendingRequests,
		PolicyStats: domain.PolicyStats{
			Mapped:   lifeMapped + healthMapped + fdMapped,
			Unmapped: lifeUnmapped + healthUnmapped + fdUnmapped + generalTotal,
		},
	}, nil
}
