package service

import (
	"context"
	"strings"
	"testing"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// superAdminSyncFixture wires two agencies, a client in each, and one inbox row belonging to the
// first agency — enough to prove a super admin acts on exactly the book they named.
type superAdminSyncFixture struct {
	svc        domain.AgencySyncService
	superAdmin *domain.User
	adminA     *domain.User
	adminB     *domain.User
	clientA    *domain.User
	clientB    *domain.User
	memberA    *domain.FamilyMember
	memberB    *domain.FamilyMember
	row        *domain.ImportedDeposit
	deposits   *fakeFixedDepositRepo
}

func newSuperAdminSyncFixture(t *testing.T) *superAdminSyncFixture {
	t.Helper()

	f := &superAdminSyncFixture{
		superAdmin: &domain.User{ID: bson.NewObjectID(), Name: "Head Office", Role: domain.RoleSuperAdmin, IsActive: true, AdminID: "ADM-ROOT00"},
		adminA:     &domain.User{ID: bson.NewObjectID(), Name: "Asha Nair", Role: domain.RoleAdmin, IsActive: true, AdminID: "ADM-ASHA01"},
		adminB:     &domain.User{ID: bson.NewObjectID(), Name: "Bikash Roy", Role: domain.RoleAdmin, IsActive: true, AdminID: "ADM-BIKASH"},
	}
	f.clientA = &domain.User{ID: bson.NewObjectID(), Name: "Riya Sen", Role: domain.RoleClient, IsActive: true, AgencyID: "ADM-ASHA01"}
	f.clientB = &domain.User{ID: bson.NewObjectID(), Name: "Sanjay Das", Role: domain.RoleClient, IsActive: true, AgencyID: "ADM-BIKASH"}
	f.memberA = &domain.FamilyMember{ID: bson.NewObjectID(), UserID: f.clientA.ID, Name: "Riya Sen"}
	f.memberB = &domain.FamilyMember{ID: bson.NewObjectID(), UserID: f.clientB.ID, Name: "Sanjay Das"}

	f.row = misRow()
	f.row.ID = bson.NewObjectID()
	f.row.AgencyID = "ADM-ASHA01"

	f.deposits = &fakeFixedDepositRepo{existing: map[string]bool{}}
	f.svc = NewAgencySyncService(nil, nil, f.deposits, newFakeImportedDepositRepo(f.row),
		newFakeFamilyRepo(f.memberA, f.memberB),
		newFakeUserRepo(f.superAdmin, f.adminA, f.adminB, f.clientA, f.clientB))

	return f
}

func TestASuperAdminMustNameTheAgencyToSyncFor(t *testing.T) {
	// A super admin has no book of their own. Silently picking one, or writing rows with no agency at
	// all, would create an inbox nobody can ever claim — so this has to be refused, clearly.
	f := newSuperAdminSyncFixture(t)
	ctx := context.Background()

	_, err := f.svc.LinkImportedDeposit(ctx, domain.RoleSuperAdmin, f.superAdmin.ID.Hex(), "", f.row.ID.Hex(),
		&domain.LinkImportedDepositDTO{UserID: f.clientA.ID.Hex(), FamilyMemberID: f.memberA.ID.Hex()})
	if err == nil {
		t.Fatal("expected a super admin with no agency named to be refused")
	}
	if !strings.Contains(err.Error(), "choose which agency") {
		t.Errorf("the error should say an agency has to be chosen, got %v", err)
	}

	if _, _, err := f.svc.ListImportedDeposits(ctx, domain.RoleSuperAdmin, f.superAdmin.ID.Hex(), "", "unclaimed", "", 1, 20); err == nil {
		t.Error("listing an inbox without naming the agency must be refused too")
	}
}

func TestASuperAdminSyncsOnBehalfOfTheNamedAgency(t *testing.T) {
	f := newSuperAdminSyncFixture(t)
	ctx := context.Background()

	// Typed in lower case, as it would arrive from a picker storing the canonical value.
	created, err := f.svc.LinkImportedDeposit(ctx, domain.RoleSuperAdmin, f.superAdmin.ID.Hex(), "adm-asha01", f.row.ID.Hex(),
		&domain.LinkImportedDepositDTO{UserID: f.clientA.ID.Hex(), FamilyMemberID: f.memberA.ID.Hex()})
	if err != nil {
		t.Fatalf("LinkImportedDeposit: %v", err)
	}
	if created.UserID != f.clientA.ID {
		t.Errorf("the deposit should be filed under that agency's client, got %s", created.UserID.Hex())
	}
	if created.ManagedBy != domain.ManagedByAgency {
		t.Errorf("a staff-linked deposit should be agency-managed, got %q", created.ManagedBy)
	}
}

func TestASuperAdminCannotCrossAgenciesWhileLinking(t *testing.T) {
	// Acting for agency A, the row belongs to A — attaching it to agency B's client would move one
	// agency's business into another's book. The role doesn't license that; the named agency decides.
	f := newSuperAdminSyncFixture(t)
	ctx := context.Background()

	_, err := f.svc.LinkImportedDeposit(ctx, domain.RoleSuperAdmin, f.superAdmin.ID.Hex(), "ADM-ASHA01", f.row.ID.Hex(),
		&domain.LinkImportedDepositDTO{UserID: f.clientB.ID.Hex(), FamilyMemberID: f.memberB.ID.Hex()})
	if err == nil {
		t.Fatal("linking another agency's client must be refused")
	}
	if f.deposits.created != nil {
		t.Error("nothing may be created when the client belongs to a different agency")
	}
}

func TestASuperAdminCannotTouchAnotherAgencysInboxRow(t *testing.T) {
	// Acting for agency B, the row belongs to A: the row itself is out of scope.
	f := newSuperAdminSyncFixture(t)
	ctx := context.Background()

	_, err := f.svc.LinkImportedDeposit(ctx, domain.RoleSuperAdmin, f.superAdmin.ID.Hex(), "ADM-BIKASH", f.row.ID.Hex(),
		&domain.LinkImportedDepositDTO{UserID: f.clientB.ID.Hex(), FamilyMemberID: f.memberB.ID.Hex()})
	if err == nil {
		t.Fatal("a row from another agency's inbox must not be linkable")
	}
	if f.deposits.created != nil {
		t.Error("nothing may be created from another agency's inbox row")
	}
}

func TestAnUnknownAgencyIDIsRefused(t *testing.T) {
	f := newSuperAdminSyncFixture(t)
	ctx := context.Background()

	_, err := f.svc.LinkImportedDeposit(ctx, domain.RoleSuperAdmin, f.superAdmin.ID.Hex(), "ADM-NOBODY", f.row.ID.Hex(),
		&domain.LinkImportedDepositDTO{UserID: f.clientA.ID.Hex(), FamilyMemberID: f.memberA.ID.Hex()})
	if err == nil {
		t.Fatal("an Agency ID nobody holds must be refused")
	}
	if !strings.Contains(err.Error(), "unknown Agency ID") {
		t.Errorf("the error should name the problem, got %v", err)
	}
}

func TestAPlainAdminsOwnAgencyAlwaysWins(t *testing.T) {
	// An admin passing another agency's ID gets their own book regardless — their scope is not theirs
	// to widen, so the row from their own agency still links and the foreign ID is simply ignored.
	f := newSuperAdminSyncFixture(t)
	ctx := context.Background()

	created, err := f.svc.LinkImportedDeposit(ctx, domain.RoleAdmin, f.adminA.ID.Hex(), "ADM-BIKASH", f.row.ID.Hex(),
		&domain.LinkImportedDepositDTO{UserID: f.clientA.ID.Hex(), FamilyMemberID: f.memberA.ID.Hex()})
	if err != nil {
		t.Fatalf("an admin's own agency should still resolve: %v", err)
	}
	if created.UserID != f.clientA.ID {
		t.Errorf("the deposit should be filed under the admin's own client, got %s", created.UserID.Hex())
	}

	// And the other way round: admin B cannot reach agency A's row even by naming it.
	f2 := newSuperAdminSyncFixture(t)
	if _, err := f2.svc.LinkImportedDeposit(ctx, domain.RoleAdmin, f2.adminB.ID.Hex(), "ADM-ASHA01", f2.row.ID.Hex(),
		&domain.LinkImportedDepositDTO{UserID: f2.clientB.ID.Hex(), FamilyMemberID: f2.memberB.ID.Hex()}); err == nil {
		t.Error("an admin must not reach another agency's inbox row by naming its Agency ID")
	}
}
