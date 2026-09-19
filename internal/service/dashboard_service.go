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
// chronologically-sorted list of every Life/Health premium, Fixed Deposit maturity, and Motor
// policy expiry due within the next 30 days — matching the "Premiums and maturities" label the
// client-facing Home screen actually shows. Each fetch runs concurrently via errgroup since they
// are independent and touch different collections.
func (s *dashboardService) GetClientDashboard(ctx context.Context, userIDStr string) (*domain.ClientDashboardDTO, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	var (
		familyTotal, lifeTotal, healthTotal, generalTotal, fdTotal int64
		lifePolicies                                               []*domain.LifeInsurance
		healthPolicies                                             []*domain.HealthInsurance
		generalPolicies                                            []*domain.GeneralInsurance
		fdPolicies                                                 []*domain.FixedDeposit
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
		policies, total, err := s.generalInsuranceRepo.FindAllByUserID(gctx, userID)
		generalPolicies, generalTotal = policies, total
		return err
	})
	g.Go(func() error {
		fds, total, err := s.fixedDepositRepo.GetByUserID(gctx, userID)
		fdPolicies, fdTotal = fds, total
		return err
	})

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("failed to load client dashboard data: %w", err)
	}

	upcoming := buildUpcomingPayments(lifePolicies, healthPolicies, generalPolicies, fdPolicies, time.Now())

	return &domain.ClientDashboardDTO{
		TotalFamilyMembers:   familyTotal,
		TotalLifePolicies:    lifeTotal,
		TotalHealthPolicies:  healthTotal,
		TotalGeneralPolicies: generalTotal,
		TotalFixedDeposits:   fdTotal,
		UpcomingPremiums:     upcoming,
	}, nil
}

// GetAdminDashboard aggregates dashboard totals. TotalActiveClients, PendingAccessRequests, and
// PolicyStats are all scoped to the caller: a super_admin gets platform-wide numbers; a plain admin
// gets counts limited to their own agency (clients/requests whose AgencyID/AppliedAgencyID matches
// their own AdminID, and policy counts restricted to those clients' policies), via the same
// resolveCallerAgencyID/canAccessAgencyScopedRecord fail-closed pattern used everywhere else.
//
// Note: General Insurance has no is_mapped field in its data model (unlike Life/Health/FD), so its
// policies can't be individually classified as mapped or unmapped. They are conservatively counted
// toward Unmapped so they still contribute to the overall total instead of silently disappearing
// from PolicyStats — worth revisiting if is_mapped tracking is ever added to General Insurance.
func (s *dashboardService) GetAdminDashboard(ctx context.Context, requesterRole, requesterID string) (*domain.AdminDashboardDTO, error) {
	var (
		activeClients                int64
		pendingRequests              int64
		lifeMapped, lifeUnmapped     int64
		healthMapped, healthUnmapped int64
		generalTotal                 int64
		fdMapped, fdUnmapped         int64
	)

	mappedFilter, unmappedFilter := true, false
	agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
	scopedButUnresolved := requesterRole == domain.RoleAdmin && agencyFilter == ""

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		if scopedButUnresolved {
			return nil // fail closed, same as UserService.GetAll / AccessRequestService.GetAllRequests
		}
		n, err := s.userRepo.CountActiveClients(gctx, agencyFilter)
		activeClients = n
		return err
	})
	g.Go(func() error {
		if scopedButUnresolved {
			return nil
		}
		_, total, err := s.accessRequestRepo.FindAll(gctx, domain.AccessStatusPending, agencyFilter, 1, 1)
		pendingRequests = total
		return err
	})
	g.Go(func() error {
		if scopedButUnresolved {
			return nil
		}
		_, total, err := s.lifeInsuranceRepo.GetAll(gctx, 1, 1, &mappedFilter, "", agencyFilter)
		lifeMapped = total
		return err
	})
	g.Go(func() error {
		if scopedButUnresolved {
			return nil
		}
		_, total, err := s.lifeInsuranceRepo.GetAll(gctx, 1, 1, &unmappedFilter, "", agencyFilter)
		lifeUnmapped = total
		return err
	})
	g.Go(func() error {
		if scopedButUnresolved {
			return nil
		}
		_, total, err := s.healthInsuranceRepo.GetAll(gctx, 1, 1, &mappedFilter, "", agencyFilter)
		healthMapped = total
		return err
	})
	g.Go(func() error {
		if scopedButUnresolved {
			return nil
		}
		_, total, err := s.healthInsuranceRepo.GetAll(gctx, 1, 1, &unmappedFilter, "", agencyFilter)
		healthUnmapped = total
		return err
	})
	g.Go(func() error {
		if scopedButUnresolved {
			return nil
		}
		_, total, err := s.generalInsuranceRepo.FindAllAdmin(gctx, 1, 1, agencyFilter)
		generalTotal = total
		return err
	})
	g.Go(func() error {
		if scopedButUnresolved {
			return nil
		}
		_, total, err := s.fixedDepositRepo.GetAll(gctx, 1, 1, &mappedFilter, agencyFilter)
		fdMapped = total
		return err
	})
	g.Go(func() error {
		if scopedButUnresolved {
			return nil
		}
		_, total, err := s.fixedDepositRepo.GetAll(gctx, 1, 1, &unmappedFilter, agencyFilter)
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
			Mapped:        lifeMapped + healthMapped + fdMapped,
			Unmapped:      lifeUnmapped + healthUnmapped + fdUnmapped,
			MotorPolicies: generalTotal,
		},
	}, nil
}

// Dashboard windows. Dates are compared as Indian calendar days, because due dates are stored at
// either 00:00 UTC (LIC sync) or 12:00 UTC (app forms) — comparing raw timestamps against "now"
// made a premium due *today* vanish as soon as the clock passed that instant.
const (
	upcomingWindowDays      = 30
	premiumOverdueLookback  = 90 // older unpaid premiums are treated as lapsed/stale, not "due"
	motorExpiredLookbackDay = 30
)

var indiaTZ = time.FixedZone("IST", 5*60*60+30*60)

// dayNumber turns an instant into a whole-day index on the Indian calendar.
func dayNumber(t time.Time) int {
	y, m, d := t.In(indiaTZ).Date()
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// buildUpcomingPayments lists what a client must act on: unpaid premiums that are overdue (up to
// premiumOverdueLookback days), anything due today through the next 30 days, and motor policies that
// expired recently or expire soon. Overdue items sort first.
func buildUpcomingPayments(
	life []*domain.LifeInsurance,
	health []*domain.HealthInsurance,
	general []*domain.GeneralInsurance,
	fds []*domain.FixedDeposit,
	now time.Time,
) []domain.UpcomingPayment {
	today := dayNumber(now)
	upcoming := make([]domain.UpcomingPayment, 0)

	premiumItem := func(kind, name string, amount float64, due, coverEnds time.Time) {
		if due.IsZero() {
			return
		}
		d := dayNumber(due)
		// A due date past the end of cover/premium term is a stale schedule, not a real demand.
		if !coverEnds.IsZero() && d > dayNumber(coverEnds) {
			return
		}
		if d < today-premiumOverdueLookback || d > today+upcomingWindowDays {
			return
		}
		upcoming = append(upcoming, domain.UpcomingPayment{
			Type: kind, EntityName: name, Amount: amount, DueDate: due, IsOverdue: d < today,
		})
	}

	for _, p := range life {
		name := p.PolicyDetails.PlanName
		if insured := p.PolicyDetails.LifeInsuredName; insured != "" {
			name = fmt.Sprintf("%s · %s", name, insured)
		}
		// Premiums stop after the premium paying term (or at maturity, whichever is first).
		coverEnds := p.PolicyDetails.MaturityDate
		if p.PolicyDetails.PPT > 0 && !p.PolicyDetails.DOC.IsZero() {
			if pptEnd := p.PolicyDetails.DOC.AddDate(p.PolicyDetails.PPT, 0, 0); coverEnds.IsZero() || pptEnd.Before(coverEnds) {
				coverEnds = pptEnd
			}
		}
		premiumItem("Life Insurance", name, p.PremiumDetails.InstallmentPremium, p.PremiumDetails.NextDueDate, coverEnds)
	}
	for _, p := range health {
		name := p.PolicyDetails.PlanName
		if insured := p.PolicyDetails.PrimaryInsuredName; insured != "" {
			name = fmt.Sprintf("%s · %s", name, insured)
		}
		premiumItem("Health Insurance", name, p.PremiumDetails.InstallmentPremium, p.PremiumDetails.NextDueDate, p.PolicyDetails.ExpiryDate)
	}
	for _, fd := range fds {
		d := dayNumber(fd.MaturityDate)
		if !fd.MaturityDate.IsZero() && d >= today && d <= today+upcomingWindowDays {
			upcoming = append(upcoming, domain.UpcomingPayment{
				Type: "Fixed Deposit", EntityName: fd.FDName, Amount: fd.MaturityAmount, DueDate: fd.MaturityDate,
			})
		}
	}
	for _, p := range general {
		due, err := time.ParseInLocation("2006-01-02", p.DateOfExpiry, indiaTZ)
		if err != nil {
			continue
		}
		d := dayNumber(due)
		if d >= today-motorExpiredLookbackDay && d <= today+upcomingWindowDays {
			upcoming = append(upcoming, domain.UpcomingPayment{
				Type: "Motor Insurance", EntityName: p.VehicleNo, DueDate: due, IsOverdue: d < today,
			})
		}
	}

	sort.SliceStable(upcoming, func(i, j int) bool {
		if upcoming[i].IsOverdue != upcoming[j].IsOverdue {
			return upcoming[i].IsOverdue
		}
		return upcoming[i].DueDate.Before(upcoming[j].DueDate)
	})
	return upcoming
}
