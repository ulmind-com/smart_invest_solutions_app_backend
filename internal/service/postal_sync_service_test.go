package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type fakeImportedDepositRepo struct {
	domain.ImportedDepositRepository
	rows map[bson.ObjectID]*domain.ImportedDeposit
}

func newFakeImportedDepositRepo(rows ...*domain.ImportedDeposit) *fakeImportedDepositRepo {
	r := &fakeImportedDepositRepo{rows: map[bson.ObjectID]*domain.ImportedDeposit{}}
	for _, row := range rows {
		if row.ID.IsZero() {
			row.ID = bson.NewObjectID()
		}
		r.rows[row.ID] = row
	}
	return r
}

func (r *fakeImportedDepositRepo) FindByID(_ context.Context, id bson.ObjectID) (*domain.ImportedDeposit, error) {
	if row, ok := r.rows[id]; ok {
		return row, nil
	}
	return nil, domain.ErrUserNotFound
}

type fakeFixedDepositRepo struct {
	domain.FixedDepositRepository
	created  *domain.FixedDeposit
	existing map[string]bool
}

func (r *fakeFixedDepositRepo) Create(_ context.Context, fd *domain.FixedDeposit) (*domain.FixedDeposit, error) {
	fd.ID = bson.NewObjectID()
	r.created = fd
	return fd, nil
}

func (r *fakeFixedDepositRepo) GetExistingFDNumbers(_ context.Context, numbers []string, _ string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, n := range numbers {
		if r.existing[n] {
			out[n] = true
		}
	}
	return out, nil
}

// linkFixture wires an agency admin, one of their clients, a family member and one inbox row.
func linkFixture(t *testing.T, row *domain.ImportedDeposit) (domain.AgencySyncService, *domain.User, *domain.FamilyMember, *fakeFixedDepositRepo, *domain.ImportedDeposit) {
	t.Helper()
	admin := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleAdmin, IsActive: true, AdminID: "ADM-AAAAAA"}
	client := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleClient, IsActive: true, AgencyID: "ADM-AAAAAA"}
	member := &domain.FamilyMember{UserID: client.ID, Name: "Self"}

	row.AgencyID = "ADM-AAAAAA"
	depositRepo := &fakeFixedDepositRepo{existing: map[string]bool{}}
	svc := NewAgencySyncService(nil, nil, depositRepo, newFakeImportedDepositRepo(row),
		newFakeFamilyRepo(member), newFakeUserRepo(admin, client))

	return svc, admin, member, depositRepo, row
}

func misRow() *domain.ImportedDeposit {
	return &domain.ImportedDeposit{
		AccountNo: "20165161217", HolderName: "ANIL KUMAR SAHA", JointHolderName: "MITALI SAHA",
		Scheme: "5YR MIS", SchemeCode: "MIS", TermMonths: 60,
		DepositAmount: 90000, MonthlyIncome: 585,
		IssueDate:    time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC),
		MaturityDate: time.Date(2030, 7, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestLinkImportedDepositCreatesTheClientsDeposit(t *testing.T) {
	row := misRow()
	svc, admin, member, repo, imported := linkFixture(t, row)
	ctx := context.Background()

	created, err := svc.LinkImportedDeposit(ctx, domain.RoleAdmin, admin.ID.Hex(), "", imported.ID.Hex(),
		&domain.LinkImportedDepositDTO{UserID: member.UserID.Hex(), FamilyMemberID: member.ID.Hex(), NomineeName: "Mitali"})
	if err != nil {
		t.Fatalf("linking failed: %v", err)
	}

	if created.FDNumber != "20165161217" || created.PrincipalAmount != 90000 {
		t.Fatalf("report figures not carried over: %+v", created)
	}
	// A monthly-income scheme returns the principal at maturity — a zero would show the client a
	// deposit that matures into nothing.
	if created.MaturityAmount != 90000 || created.MonthlyIncome != 585 {
		t.Fatalf("monthly-income deposit valued wrong: %+v", created)
	}
	if created.SecondHolderName != "MITALI SAHA" {
		t.Fatalf("joint holder should carry over, got %q", created.SecondHolderName)
	}
	if created.AccountType != "Post Office — MIS" || created.CompanyName != defaultDepositInstitution {
		t.Fatalf("scheme labelling wrong: %+v", created)
	}
	if created.Term != 60 {
		t.Fatalf("term should come from the report, got %d", created.Term)
	}
	// Filed by the agency: the client sees it as advisor-managed and can't edit it away.
	if created.ManagedBy != domain.ManagedByAgency || !created.IsMapped {
		t.Fatalf("a linked deposit is agency-managed: %+v", created)
	}
	if repo.created == nil {
		t.Fatal("deposit was never written")
	}
}

func TestLinkImportedDepositNeedsAMaturityDate(t *testing.T) {
	row := misRow()
	row.MaturityDate = time.Time{} // some rows print no maturity date at all
	svc, admin, member, _, imported := linkFixture(t, row)
	ctx := context.Background()

	_, err := svc.LinkImportedDeposit(ctx, domain.RoleAdmin, admin.ID.Hex(), "", imported.ID.Hex(),
		&domain.LinkImportedDepositDTO{UserID: member.UserID.Hex(), FamilyMemberID: member.ID.Hex()})
	if err == nil || !strings.Contains(err.Error(), "maturity date") {
		t.Fatalf("expected the admin to be asked for a maturity date, got %v", err)
	}

	// Supplying one completes the link.
	supplied := time.Date(2030, 7, 1, 0, 0, 0, 0, time.UTC)
	created, err := svc.LinkImportedDeposit(ctx, domain.RoleAdmin, admin.ID.Hex(), "", imported.ID.Hex(),
		&domain.LinkImportedDepositDTO{UserID: member.UserID.Hex(), FamilyMemberID: member.ID.Hex(), MaturityDate: &supplied})
	if err != nil {
		t.Fatalf("linking with a supplied maturity date failed: %v", err)
	}
	if !created.MaturityDate.Equal(supplied) {
		t.Fatalf("supplied maturity date not used: %+v", created)
	}
}

func TestLinkImportedDepositStaysInsideTheAgency(t *testing.T) {
	row := misRow()
	svc, admin, member, _, imported := linkFixture(t, row)
	ctx := context.Background()

	// A row belonging to another agency is invisible, even with a valid id.
	imported.AgencyID = "ADM-OTHER0"
	if _, err := svc.LinkImportedDeposit(ctx, domain.RoleAdmin, admin.ID.Hex(), "", imported.ID.Hex(),
		&domain.LinkImportedDepositDTO{UserID: member.UserID.Hex(), FamilyMemberID: member.ID.Hex()}); err == nil {
		t.Fatal("another agency's inbox row must not be linkable")
	}
	imported.AgencyID = "ADM-AAAAAA"

	// So is a client who belongs to someone else's agency.
	outsider := bson.NewObjectID()
	if _, err := svc.LinkImportedDeposit(ctx, domain.RoleAdmin, admin.ID.Hex(), "", imported.ID.Hex(),
		&domain.LinkImportedDepositDTO{UserID: outsider.Hex(), FamilyMemberID: member.ID.Hex()}); err == nil {
		t.Fatal("a client outside the agency must not be linkable")
	}

	// And a family member who belongs to a different client.
	stranger := &domain.FamilyMember{ID: bson.NewObjectID(), UserID: bson.NewObjectID(), Name: "Someone else"}
	svcWithStranger := NewAgencySyncService(nil, nil, &fakeFixedDepositRepo{}, newFakeImportedDepositRepo(imported),
		newFakeFamilyRepo(stranger), newFakeUserRepo(
			&domain.User{ID: admin.ID, Role: domain.RoleAdmin, IsActive: true, AdminID: "ADM-AAAAAA"},
			&domain.User{ID: member.UserID, Role: domain.RoleClient, IsActive: true, AgencyID: "ADM-AAAAAA"},
		))
	if _, err := svcWithStranger.LinkImportedDeposit(ctx, domain.RoleAdmin, admin.ID.Hex(), "", imported.ID.Hex(),
		&domain.LinkImportedDepositDTO{UserID: member.UserID.Hex(), FamilyMemberID: stranger.ID.Hex()}); err == nil {
		t.Fatal("a family member of another client must not be linkable")
	}
}
