package service

import (
	"context"
	"testing"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// fakeRenewalRepo serves fixed rows and records the window and agency it was asked for.
type fakeRenewalRepo struct {
	domain.RenewalRepository
	life     []*domain.LifeInsuranceWithCustomer
	health   []*domain.HealthInsuranceWithCustomer
	motor    []*domain.GeneralInsuranceWithCustomer
	deposits []*domain.FixedDepositWithCustomer

	askedAgency string
	askedFrom   time.Time
	askedTo     time.Time
}

func (r *fakeRenewalRepo) LifeDue(_ context.Context, from, to time.Time, agencyID string) ([]*domain.LifeInsuranceWithCustomer, error) {
	r.askedAgency, r.askedFrom, r.askedTo = agencyID, from, to
	return r.life, nil
}

func (r *fakeRenewalRepo) HealthDue(_ context.Context, _, _ time.Time, _ string) ([]*domain.HealthInsuranceWithCustomer, error) {
	return r.health, nil
}

func (r *fakeRenewalRepo) MotorExpiring(_ context.Context, _, _, _ string) ([]*domain.GeneralInsuranceWithCustomer, error) {
	return r.motor, nil
}

func (r *fakeRenewalRepo) DepositsMaturing(_ context.Context, _, _ time.Time, _ string) ([]*domain.FixedDepositWithCustomer, error) {
	return r.deposits, nil
}

// inDays is a date the given number of whole Indian-calendar days from today.
func inDays(days int) time.Time {
	return dayStart(time.Now()).AddDate(0, 0, days)
}

func renewalStaff() (*domain.User, *domain.User, *domain.User) {
	superAdmin := &domain.User{ID: bson.NewObjectID(), Name: "Head Office", Role: domain.RoleSuperAdmin, IsActive: true, AdminID: "ADM-ROOT00"}
	adminA := &domain.User{ID: bson.NewObjectID(), Name: "Asha Nair", Role: domain.RoleAdmin, IsActive: true, AdminID: "ADM-ASHA01"}
	adminB := &domain.User{ID: bson.NewObjectID(), Name: "Bikash Roy", Role: domain.RoleAdmin, IsActive: true, AdminID: "ADM-BIKASH"}
	return superAdmin, adminA, adminB
}

func TestRenewalsReportDaysLeftAndWhoIsBehindEachRow(t *testing.T) {
	superAdmin, adminA, _ := renewalStaff()
	repo := &fakeRenewalRepo{
		life: []*domain.LifeInsuranceWithCustomer{{
			ID:             bson.NewObjectID(),
			UserID:         bson.NewObjectID(),
			CompanyName:    "LIC",
			CustomerName:   "Riya Sen",
			ContactNo:      "9876500000",
			AgencyID:       "ADM-ASHA01",
			PolicyDetails:  domain.PolicyDetails{PolicyNo: "123456789", PlanName: "Jeevan Anand", LifeInsuredName: "Riya Sen"},
			PremiumDetails: domain.PremiumDetails{InstallmentPremium: 12000, NextDueDate: inDays(5)},
		}},
	}
	svc := NewRenewalService(repo, newFakeUserRepo(superAdmin, adminA))

	page, err := svc.GetRenewals(context.Background(), domain.RoleSuperAdmin, superAdmin.ID.Hex(), domain.RenewalQuery{})
	if err != nil {
		t.Fatalf("GetRenewals: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected one row, got %d", len(page.Items))
	}

	row := page.Items[0]
	if row.DaysLeft != 5 || row.IsOverdue {
		t.Errorf("days left = %d overdue = %t, want 5 and not overdue", row.DaysLeft, row.IsOverdue)
	}
	if row.Label != "Premium due" || row.Amount != 12000 {
		t.Errorf("unexpected label/amount: %q / %v", row.Label, row.Amount)
	}
	if row.ClientName != "Riya Sen" || row.ClientPhone != "9876500000" {
		t.Errorf("the row must name the client who holds it, got %q / %q", row.ClientName, row.ClientPhone)
	}
	// The whole point for a super admin: which admin is behind this renewal.
	if row.AgencyID != "ADM-ASHA01" || row.AgencyName != "Asha Nair" {
		t.Errorf("the row must name the managing admin, got %q / %q", row.AgencyID, row.AgencyName)
	}
	if row.Reference != "123456789" || row.Title != "Jeevan Anand" {
		t.Errorf("unexpected title/reference: %q / %q", row.Title, row.Reference)
	}
}

func TestRenewalsSortMostOverdueFirstThenSoonest(t *testing.T) {
	superAdmin, adminA, _ := renewalStaff()
	mk := func(name string, due time.Time) *domain.LifeInsuranceWithCustomer {
		return &domain.LifeInsuranceWithCustomer{
			ID: bson.NewObjectID(), UserID: bson.NewObjectID(), CustomerName: name, AgencyID: "ADM-ASHA01",
			PolicyDetails:  domain.PolicyDetails{PlanName: "Plan"},
			PremiumDetails: domain.PremiumDetails{NextDueDate: due},
		}
	}
	repo := &fakeRenewalRepo{life: []*domain.LifeInsuranceWithCustomer{
		mk("Soon", inDays(2)),
		mk("Late", inDays(-30)),
		mk("Later", inDays(45)),
		mk("Slightly late", inDays(-1)),
	}}
	svc := NewRenewalService(repo, newFakeUserRepo(superAdmin, adminA))

	page, err := svc.GetRenewals(context.Background(), domain.RoleSuperAdmin, superAdmin.ID.Hex(), domain.RenewalQuery{})
	if err != nil {
		t.Fatalf("GetRenewals: %v", err)
	}
	want := []string{"Late", "Slightly late", "Soon", "Later"}
	for i, name := range want {
		if page.Items[i].ClientName != name {
			t.Errorf("position %d = %q, want %q (full order: %v)", i, page.Items[i].ClientName, name, renewalNames(page.Items))
		}
	}
}

func renewalNames(rows []*domain.RenewalRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ClientName)
	}
	return out
}

func TestRenewalWindowsIncludeWhatIsAlreadyOverdue(t *testing.T) {
	// An admin working "the next 7 days" has to see what they already missed, or the list hides the
	// most urgent work of all.
	superAdmin, adminA, _ := renewalStaff()
	mk := func(name string, days int) *domain.LifeInsuranceWithCustomer {
		return &domain.LifeInsuranceWithCustomer{
			ID: bson.NewObjectID(), UserID: bson.NewObjectID(), CustomerName: name, AgencyID: "ADM-ASHA01",
			PolicyDetails:  domain.PolicyDetails{PlanName: "Plan"},
			PremiumDetails: domain.PremiumDetails{NextDueDate: inDays(days)},
		}
	}
	repo := &fakeRenewalRepo{life: []*domain.LifeInsuranceWithCustomer{
		mk("overdue", -10), mk("in3", 3), mk("in20", 20), mk("in60", 60),
	}}
	svc := NewRenewalService(repo, newFakeUserRepo(superAdmin, adminA))
	ctx := context.Background()

	cases := []struct {
		window string
		want   int
	}{
		{domain.RenewalWindowOverdue, 1},
		{domain.RenewalWindow7, 2},  // overdue + in3
		{domain.RenewalWindow30, 3}, // + in20
		{domain.RenewalWindow90, 4}, // + in60
		{domain.RenewalWindowAll, 4},
		{"nonsense", 4}, // an unknown window reads as "no filter", not as "nothing"
	}
	for _, tc := range cases {
		page, err := svc.GetRenewals(ctx, domain.RoleSuperAdmin, superAdmin.ID.Hex(), domain.RenewalQuery{Window: tc.window})
		if err != nil {
			t.Fatalf("window %q: %v", tc.window, err)
		}
		if int(page.Total) != tc.want {
			t.Errorf("window %q returned %d rows, want %d", tc.window, page.Total, tc.want)
		}
	}
}

func TestTheRenewalSummaryDescribesTheWholeScopeNotThePage(t *testing.T) {
	// The counters must hold still while the reader switches windows — otherwise every tap changes
	// the numbers they are using to decide what to work on.
	superAdmin, adminA, _ := renewalStaff()
	repo := &fakeRenewalRepo{
		life: []*domain.LifeInsuranceWithCustomer{{
			ID: bson.NewObjectID(), UserID: bson.NewObjectID(), CustomerName: "Overdue client", AgencyID: "ADM-ASHA01",
			PolicyDetails:  domain.PolicyDetails{PlanName: "Plan"},
			PremiumDetails: domain.PremiumDetails{InstallmentPremium: 5000, NextDueDate: inDays(-4)},
		}},
		motor: []*domain.GeneralInsuranceWithCustomer{{
			ID: bson.NewObjectID(), UserID: bson.NewObjectID(), CustomerName: "Motor client",
			VehicleNo: "WB02AB1234", DateOfExpiry: isoDate(inDays(10)),
		}},
		deposits: []*domain.FixedDepositWithCustomer{{
			ID: bson.NewObjectID(), UserID: bson.NewObjectID(), CustomerName: "Deposit client", AgencyID: "ADM-ASHA01",
			FDName: "5YR MIS", MaturityAmount: 90000, MaturityDate: inDays(20),
		}},
	}
	svc := NewRenewalService(repo, newFakeUserRepo(superAdmin, adminA))

	page, err := svc.GetRenewals(context.Background(), domain.RoleSuperAdmin, superAdmin.ID.Hex(),
		domain.RenewalQuery{Window: domain.RenewalWindowOverdue})
	if err != nil {
		t.Fatalf("GetRenewals: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("the overdue window should return 1 row, got %d", page.Total)
	}

	s := page.Summary
	if s.Overdue != 1 || s.AmountOverdue != 5000 {
		t.Errorf("overdue summary = %d / %v, want 1 / 5000", s.Overdue, s.AmountOverdue)
	}
	if s.DueIn30 != 2 || s.DueIn90 != 2 {
		t.Errorf("upcoming buckets = %d / %d, want 2 / 2 regardless of the window", s.DueIn30, s.DueIn90)
	}
	if s.Life != 1 || s.Motor != 1 || s.Deposits != 1 || s.Health != 0 {
		t.Errorf("per-instrument counts wrong: %+v", s)
	}
	// The motor client has no agency — exactly what a super admin needs flagged, since nobody is
	// currently chasing that renewal.
	if s.Unassigned != 1 {
		t.Errorf("unassigned = %d, want 1", s.Unassigned)
	}
}

func TestARenewalListIsScopedToTheCallersAgency(t *testing.T) {
	superAdmin, adminA, adminB := renewalStaff()
	repo := &fakeRenewalRepo{}
	svc := NewRenewalService(repo, newFakeUserRepo(superAdmin, adminA, adminB))
	ctx := context.Background()

	// A plain admin's own agency replaces whatever they ask for.
	if _, err := svc.GetRenewals(ctx, domain.RoleAdmin, adminA.ID.Hex(), domain.RenewalQuery{AgencyID: "ADM-BIKASH"}); err != nil {
		t.Fatalf("GetRenewals: %v", err)
	}
	if repo.askedAgency != "ADM-ASHA01" {
		t.Errorf("an admin must be pinned to their own agency, got %q", repo.askedAgency)
	}

	// A super admin gets what they asked for, including the unassigned sentinel.
	for _, want := range []string{"ADM-BIKASH", domain.ClientMapAgencyUnassigned, ""} {
		if _, err := svc.GetRenewals(ctx, domain.RoleSuperAdmin, superAdmin.ID.Hex(), domain.RenewalQuery{AgencyID: want}); err != nil {
			t.Fatalf("GetRenewals(%q): %v", want, err)
		}
		if repo.askedAgency != want {
			t.Errorf("super admin agency filter = %q, want %q", repo.askedAgency, want)
		}
	}

	// A client never reaches the cross-account query at all.
	client := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleClient}
	svcForClient := NewRenewalService(&fakeRenewalRepo{}, newFakeUserRepo(client))
	page, err := svcForClient.GetRenewals(ctx, domain.RoleClient, client.ID.Hex(), domain.RenewalQuery{})
	if err != nil {
		t.Fatalf("GetRenewals: %v", err)
	}
	if len(page.Items) != 0 || page.Summary == nil {
		t.Error("a client must get an empty, well-formed page")
	}
}

func TestAPremiumPastTheEndOfCoverIsNotADemand(t *testing.T) {
	// A stale schedule is not money owed. The client's own dashboard applies this rule, so the
	// renewal book has to agree — otherwise an admin chases a premium the client doesn't owe.
	superAdmin, adminA, _ := renewalStaff()
	repo := &fakeRenewalRepo{life: []*domain.LifeInsuranceWithCustomer{
		{
			ID: bson.NewObjectID(), UserID: bson.NewObjectID(), CustomerName: "Matured", AgencyID: "ADM-ASHA01",
			PolicyDetails: domain.PolicyDetails{
				PlanName:     "Finished plan",
				MaturityDate: inDays(-400),
			},
			PremiumDetails: domain.PremiumDetails{NextDueDate: inDays(-10)},
		},
		{
			ID: bson.NewObjectID(), UserID: bson.NewObjectID(), CustomerName: "Term ended", AgencyID: "ADM-ASHA01",
			PolicyDetails: domain.PolicyDetails{
				PlanName: "Paid up",
				// A 5-year premium paying term that started 10 years ago: nothing more is payable.
				PPT: 5,
				DOC: inDays(-3650),
			},
			PremiumDetails: domain.PremiumDetails{NextDueDate: inDays(3)},
		},
		{
			ID: bson.NewObjectID(), UserID: bson.NewObjectID(), CustomerName: "Still paying", AgencyID: "ADM-ASHA01",
			PolicyDetails: domain.PolicyDetails{
				PlanName: "Live plan",
				PPT:      20,
				DOC:      inDays(-365),
			},
			PremiumDetails: domain.PremiumDetails{NextDueDate: inDays(3)},
		},
	}}
	svc := NewRenewalService(repo, newFakeUserRepo(superAdmin, adminA))

	page, err := svc.GetRenewals(context.Background(), domain.RoleSuperAdmin, superAdmin.ID.Hex(), domain.RenewalQuery{})
	if err != nil {
		t.Fatalf("GetRenewals: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ClientName != "Still paying" {
		t.Errorf("only the live policy should appear, got %v", renewalNames(page.Items))
	}
}

func TestAHealthPolicyShowsWhicheverComesFirst(t *testing.T) {
	// Chasing a premium due next month is pointless if the cover lapses next week.
	superAdmin, adminA, _ := renewalStaff()
	repo := &fakeRenewalRepo{health: []*domain.HealthInsuranceWithCustomer{
		{
			ID: bson.NewObjectID(), UserID: bson.NewObjectID(), CustomerName: "Expiring first", AgencyID: "ADM-ASHA01",
			PolicyDetails:  domain.HealthPolicyDetails{PlanName: "Family floater", ExpiryDate: inDays(6)},
			PremiumDetails: domain.HealthPremiumDetails{InstallmentPremium: 9000, NextDueDate: inDays(40)},
		},
		{
			ID: bson.NewObjectID(), UserID: bson.NewObjectID(), CustomerName: "Premium first", AgencyID: "ADM-ASHA01",
			PolicyDetails:  domain.HealthPolicyDetails{PlanName: "Individual", ExpiryDate: inDays(80)},
			PremiumDetails: domain.HealthPremiumDetails{InstallmentPremium: 4000, NextDueDate: inDays(9)},
		},
	}}
	svc := NewRenewalService(repo, newFakeUserRepo(superAdmin, adminA))

	page, err := svc.GetRenewals(context.Background(), domain.RoleSuperAdmin, superAdmin.ID.Hex(), domain.RenewalQuery{})
	if err != nil {
		t.Fatalf("GetRenewals: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected both policies, got %d", len(page.Items))
	}

	first, second := page.Items[0], page.Items[1]
	if first.ClientName != "Expiring first" || first.Label != "Policy expires" || first.DaysLeft != 6 {
		t.Errorf("the expiring policy should lead, as an expiry: %+v", first)
	}
	if first.Amount != 0 {
		t.Errorf("an expiry row carries no premium to collect, got %v", first.Amount)
	}
	if second.Label != "Premium due" || second.DaysLeft != 9 || second.Amount != 4000 {
		t.Errorf("the other should read as a premium: %+v", second)
	}
}

func TestRenewalsCanBeNarrowedByInstrumentAndSearch(t *testing.T) {
	superAdmin, adminA, _ := renewalStaff()
	repo := &fakeRenewalRepo{
		life: []*domain.LifeInsuranceWithCustomer{{
			ID: bson.NewObjectID(), UserID: bson.NewObjectID(), CustomerName: "Riya Sen", AgencyID: "ADM-ASHA01",
			PolicyDetails:  domain.PolicyDetails{PlanName: "Jeevan Anand", PolicyNo: "123456789"},
			PremiumDetails: domain.PremiumDetails{NextDueDate: inDays(4)},
		}},
		motor: []*domain.GeneralInsuranceWithCustomer{{
			ID: bson.NewObjectID(), UserID: bson.NewObjectID(), CustomerName: "Sanjay Das", AgencyID: "ADM-ASHA01",
			VehicleNo: "WB02AB1234", PolicyNo: "MTR-889120", DateOfExpiry: isoDate(inDays(8)),
		}},
	}
	svc := NewRenewalService(repo, newFakeUserRepo(superAdmin, adminA))
	ctx := context.Background()

	motorOnly, err := svc.GetRenewals(ctx, domain.RoleSuperAdmin, superAdmin.ID.Hex(),
		domain.RenewalQuery{Kind: domain.RenewalKindMotor})
	if err != nil {
		t.Fatalf("GetRenewals: %v", err)
	}
	if motorOnly.Total != 1 || motorOnly.Items[0].Title != "WB02AB1234" {
		t.Errorf("the motor filter should leave one motor row, got %v", renewalNames(motorOnly.Items))
	}

	// Search reaches the client, the title and the number on the paperwork.
	for _, needle := range []string{"riya", "jeevan", "123456789", "ASHA"} {
		found, err := svc.GetRenewals(ctx, domain.RoleSuperAdmin, superAdmin.ID.Hex(), domain.RenewalQuery{Search: needle})
		if err != nil {
			t.Fatalf("search %q: %v", needle, err)
		}
		if found.Total == 0 {
			t.Errorf("search %q found nothing", needle)
		}
	}

	none, err := svc.GetRenewals(ctx, domain.RoleSuperAdmin, superAdmin.ID.Hex(), domain.RenewalQuery{Search: "nobody"})
	if err != nil {
		t.Fatalf("GetRenewals: %v", err)
	}
	if none.Total != 0 || none.Items == nil {
		t.Error("a search matching nothing should return an empty, non-nil list")
	}
}

func TestRenewalPagingIsClampedAndSurvivesAPagePastTheEnd(t *testing.T) {
	superAdmin, adminA, _ := renewalStaff()
	rows := make([]*domain.LifeInsuranceWithCustomer, 0, 5)
	for i := 0; i < 5; i++ {
		rows = append(rows, &domain.LifeInsuranceWithCustomer{
			ID: bson.NewObjectID(), UserID: bson.NewObjectID(), CustomerName: "Client", AgencyID: "ADM-ASHA01",
			PolicyDetails:  domain.PolicyDetails{PlanName: "Plan"},
			PremiumDetails: domain.PremiumDetails{NextDueDate: inDays(i + 1)},
		})
	}
	repo := &fakeRenewalRepo{life: rows}
	svc := NewRenewalService(repo, newFakeUserRepo(superAdmin, adminA))
	ctx := context.Background()

	first, err := svc.GetRenewals(ctx, domain.RoleSuperAdmin, superAdmin.ID.Hex(), domain.RenewalQuery{Page: 1, Limit: 2})
	if err != nil {
		t.Fatalf("GetRenewals: %v", err)
	}
	if len(first.Items) != 2 || first.Total != 5 || first.TotalPages != 3 {
		t.Errorf("page 1 = %d rows, total %d, pages %d; want 2/5/3", len(first.Items), first.Total, first.TotalPages)
	}

	last, err := svc.GetRenewals(ctx, domain.RoleSuperAdmin, superAdmin.ID.Hex(), domain.RenewalQuery{Page: 3, Limit: 2})
	if err != nil {
		t.Fatalf("GetRenewals: %v", err)
	}
	if len(last.Items) != 1 {
		t.Errorf("the last page should hold the remaining row, got %d", len(last.Items))
	}

	// Past the end: empty, not a crash and not a wrapped page.
	beyond, err := svc.GetRenewals(ctx, domain.RoleSuperAdmin, superAdmin.ID.Hex(), domain.RenewalQuery{Page: 99, Limit: 2})
	if err != nil {
		t.Fatalf("GetRenewals: %v", err)
	}
	if len(beyond.Items) != 0 {
		t.Errorf("a page past the end should be empty, got %d rows", len(beyond.Items))
	}

	// An absurd limit is clamped rather than honoured.
	clamped, err := svc.GetRenewals(ctx, domain.RoleSuperAdmin, superAdmin.ID.Hex(), domain.RenewalQuery{Limit: 100000})
	if err != nil {
		t.Fatalf("GetRenewals: %v", err)
	}
	if clamped.Limit != renewalDefaultLimit {
		t.Errorf("limit = %d, want the default %d", clamped.Limit, renewalDefaultLimit)
	}
}

func TestTheRenewalFetchCoversOverdueAndTheWholeHorizon(t *testing.T) {
	// The window on screen must not narrow the fetch: the summary counts every bucket, so a fetch
	// limited to the selected window would make the other counts lie.
	superAdmin, adminA, _ := renewalStaff()
	repo := &fakeRenewalRepo{}
	svc := NewRenewalService(repo, newFakeUserRepo(superAdmin, adminA))

	if _, err := svc.GetRenewals(context.Background(), domain.RoleSuperAdmin, superAdmin.ID.Hex(),
		domain.RenewalQuery{Window: domain.RenewalWindow7}); err != nil {
		t.Fatalf("GetRenewals: %v", err)
	}

	today := dayNumber(time.Now())
	if got := today - dayNumber(repo.askedFrom); got != premiumOverdueLookback {
		t.Errorf("fetch reaches %d days back, want %d", got, premiumOverdueLookback)
	}
	if got := dayNumber(repo.askedTo) - today; got != domain.RenewalHorizonDays {
		t.Errorf("fetch reaches %d days ahead, want %d", got, domain.RenewalHorizonDays)
	}
}
