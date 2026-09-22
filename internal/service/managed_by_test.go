package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

/* ---------------------------------------------------------------- fakes */

type fakeFamilyRepo struct {
	domain.FamilyMemberRepository
	members map[bson.ObjectID]*domain.FamilyMember
}

func newFakeFamilyRepo(members ...*domain.FamilyMember) *fakeFamilyRepo {
	r := &fakeFamilyRepo{members: map[bson.ObjectID]*domain.FamilyMember{}}
	for _, m := range members {
		if m.ID.IsZero() {
			m.ID = bson.NewObjectID()
		}
		r.members[m.ID] = m
	}
	return r
}

func (r *fakeFamilyRepo) FindByID(_ context.Context, id bson.ObjectID) (*domain.FamilyMember, error) {
	if m, ok := r.members[id]; ok {
		return m, nil
	}
	return nil, domain.ErrUserNotFound
}

type fakeLifeRepo struct {
	domain.LifeInsuranceRepository
	created  *domain.LifeInsurance
	policies map[bson.ObjectID]*domain.LifeInsurance
	lastDTO  *domain.UpdateLifeInsuranceDTO
}

func (r *fakeLifeRepo) Create(_ context.Context, policy *domain.LifeInsurance) (*domain.LifeInsurance, error) {
	policy.ID = bson.NewObjectID()
	r.created = policy
	return policy, nil
}

func (r *fakeLifeRepo) GetByID(_ context.Context, id bson.ObjectID) (*domain.LifeInsurance, error) {
	if p, ok := r.policies[id]; ok {
		return p, nil
	}
	return nil, domain.ErrUserNotFound
}

func (r *fakeLifeRepo) Update(_ context.Context, id bson.ObjectID, dto *domain.UpdateLifeInsuranceDTO) (*domain.LifeInsurance, error) {
	r.lastDTO = dto
	policy := r.policies[id]
	if dto.ManagedBy != nil {
		policy.ManagedBy = *dto.ManagedBy
	}
	if dto.IsMapped != nil {
		policy.IsMapped = *dto.IsMapped
	}
	return policy, nil
}

/* ---------------------------------------------------------------- tests */

func lifeDTO(memberID bson.ObjectID) *domain.CreateLifeInsuranceDTO {
	return &domain.CreateLifeInsuranceDTO{
		FamilyMemberID: memberID.Hex(),
		CompanyName:    "LIC of India",
		PolicyDetails: domain.CreatePolicyDetailsDTO{
			PolicyNo: "123456789", PlanName: "Jeevan Anand", NomineeName: "Spouse",
			SumAssured: 500000, Term: 20, PPT: 15,
			DOC:          time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			MaturityDate: time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		PremiumDetails: domain.CreatePremiumDetailsDTO{
			InstallmentPremium: 25000,
			NextDueDate:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			PaymentMode:        domain.PaymentModeYearly,
		},
	}
}

// A client adding their own policy keeps it self-managed; an admin filing one for that client makes
// it agency-managed. That flag is what the client app's badge reads.
func TestCreateStampsWhoManagesTheRecord(t *testing.T) {
	admin := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleAdmin, IsActive: true, AdminID: "ADM-AAAAAA"}
	client := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleClient, IsActive: true, AgencyID: "ADM-AAAAAA"}
	member := &domain.FamilyMember{UserID: client.ID, Name: "Self"}
	userRepo := newFakeUserRepo(admin, client)
	familyRepo := newFakeFamilyRepo(member)
	ctx := context.Background()

	selfRepo := &fakeLifeRepo{}
	selfSvc := NewLifeInsuranceService(selfRepo, userRepo, familyRepo)
	dto := lifeDTO(member.ID)
	dto.IsMapped = true // a client can claim anything; both agency fields must be ignored
	if _, err := selfSvc.CreatePolicy(ctx, domain.RoleClient, client.ID.Hex(), dto); err != nil {
		t.Fatalf("client create failed: %v", err)
	}
	if selfRepo.created.ManagedBy != domain.ManagedByClient {
		t.Fatalf("a client's own policy should be client-managed, got %q", selfRepo.created.ManagedBy)
	}
	if selfRepo.created.IsMapped {
		t.Fatal("a client must not be able to map their own policy to the agency")
	}

	agencyRepo := &fakeLifeRepo{}
	agencySvc := NewLifeInsuranceService(agencyRepo, userRepo, familyRepo)
	adminDTO := lifeDTO(member.ID)
	adminDTO.UserID = client.ID.Hex()
	if _, err := agencySvc.CreatePolicy(ctx, domain.RoleAdmin, admin.ID.Hex(), adminDTO); err != nil {
		t.Fatalf("admin create failed: %v", err)
	}
	if agencyRepo.created.ManagedBy != domain.ManagedByAgency {
		t.Fatalf("a policy filed by an admin should be agency-managed, got %q", agencyRepo.created.ManagedBy)
	}
}

// Handing a record over (or back) is an agency decision — a client's own request is ignored.
func TestUpdateManagedByIsStaffOnly(t *testing.T) {
	admin := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleAdmin, IsActive: true, AdminID: "ADM-AAAAAA"}
	client := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleClient, IsActive: true, AgencyID: "ADM-AAAAAA"}
	member := &domain.FamilyMember{UserID: client.ID, Name: "Self"}
	userRepo := newFakeUserRepo(admin, client)
	familyRepo := newFakeFamilyRepo(member)
	ctx := context.Background()

	policy := &domain.LifeInsurance{
		ID: bson.NewObjectID(), UserID: client.ID, FamilyMemberID: member.ID,
		ManagedBy: domain.ManagedByClient,
		PolicyDetails: domain.PolicyDetails{
			Term: 20, PPT: 15,
			DOC:          time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			MaturityDate: time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	repo := &fakeLifeRepo{policies: map[bson.ObjectID]*domain.LifeInsurance{policy.ID: policy}}
	svc := NewLifeInsuranceService(repo, userRepo, familyRepo)

	agency := domain.ManagedByAgency
	if _, err := svc.UpdatePolicy(ctx, domain.RoleClient, client.ID.Hex(), policy.ID.Hex(),
		&domain.UpdateLifeInsuranceDTO{ManagedBy: &agency}); err != nil {
		t.Fatalf("client update failed: %v", err)
	}
	if repo.lastDTO.ManagedBy != nil || policy.ManagedBy != domain.ManagedByClient {
		t.Fatal("a client must not be able to mark their record agency-managed")
	}

	if _, err := svc.UpdatePolicy(ctx, domain.RoleAdmin, admin.ID.Hex(), policy.ID.Hex(),
		&domain.UpdateLifeInsuranceDTO{ManagedBy: &agency}); err != nil {
		t.Fatalf("admin update failed: %v", err)
	}
	if policy.ManagedBy != domain.ManagedByAgency {
		t.Fatalf("an admin taking over should make it agency-managed, got %q", policy.ManagedBy)
	}
}

func TestManagedByHelpers(t *testing.T) {
	if domain.ManagedByForRole(domain.RoleAdmin) != domain.ManagedByAgency ||
		domain.ManagedByForRole(domain.RoleSuperAdmin) != domain.ManagedByAgency {
		t.Fatal("staff-filed records are agency-managed")
	}
	if domain.ManagedByForRole(domain.RoleClient) != domain.ManagedByClient ||
		domain.ManagedByForRole(domain.RoleAdvisor) != domain.ManagedByClient {
		t.Fatal("client-filed records are client-managed")
	}
	// Records written before the field existed read back empty and must default to client-managed.
	if domain.NormalizeManagedBy("") != domain.ManagedByClient ||
		domain.NormalizeManagedBy("nonsense") != domain.ManagedByClient {
		t.Fatal("an unknown value must fall back to client-managed")
	}
	if domain.NormalizeManagedBy(domain.ManagedByAgency) != domain.ManagedByAgency {
		t.Fatal("a valid value must be kept")
	}
}

// A record the agency maintains is read-only for the client: the app shows it as "Managed by
// advisor", so letting the client edit or delete it anyway would desync the agency's book.
func TestClientCannotChangeAgencyManagedRecord(t *testing.T) {
	admin := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleAdmin, IsActive: true, AdminID: "ADM-AAAAAA"}
	client := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleClient, IsActive: true, AgencyID: "ADM-AAAAAA"}
	member := &domain.FamilyMember{UserID: client.ID, Name: "Self"}
	userRepo := newFakeUserRepo(admin, client)
	familyRepo := newFakeFamilyRepo(member)
	ctx := context.Background()

	policy := &domain.LifeInsurance{
		ID: bson.NewObjectID(), UserID: client.ID, FamilyMemberID: member.ID,
		ManagedBy: domain.ManagedByAgency,
		PolicyDetails: domain.PolicyDetails{
			Term: 20, PPT: 15,
			DOC:          time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			MaturityDate: time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	repo := &fakeLifeRepo{policies: map[bson.ObjectID]*domain.LifeInsurance{policy.ID: policy}}
	svc := NewLifeInsuranceService(repo, userRepo, familyRepo)

	plan := "Renamed by the client"
	_, err := svc.UpdatePolicy(ctx, domain.RoleClient, client.ID.Hex(), policy.ID.Hex(),
		&domain.UpdateLifeInsuranceDTO{PlanName: &plan})
	if err == nil || !strings.Contains(err.Error(), "advisor manages this record") {
		t.Fatalf("a client must not edit an agency-managed policy, got %v", err)
	}

	if err := svc.DeletePolicy(ctx, domain.RoleClient, client.ID.Hex(), policy.ID.Hex()); err == nil {
		t.Fatal("a client must not delete an agency-managed policy")
	}

	// The advisor who maintains it still can.
	if _, err := svc.UpdatePolicy(ctx, domain.RoleAdmin, admin.ID.Hex(), policy.ID.Hex(),
		&domain.UpdateLifeInsuranceDTO{PlanName: &plan}); err != nil {
		t.Fatalf("the agency must still be able to edit its own record: %v", err)
	}

	// And a client's own record stays theirs to change.
	policy.ManagedBy = domain.ManagedByClient
	if _, err := svc.UpdatePolicy(ctx, domain.RoleClient, client.ID.Hex(), policy.ID.Hex(),
		&domain.UpdateLifeInsuranceDTO{PlanName: &plan}); err != nil {
		t.Fatalf("a client must still be able to edit their own record: %v", err)
	}
}

func TestGetMyAdvisorResolvesTheAgencyAdmin(t *testing.T) {
	admin := &domain.User{
		ID: bson.NewObjectID(), Role: domain.RoleAdmin, IsActive: true, AdminID: "ADM-AAAAAA",
		Name: "Asha Nair", Email: "asha@agency.in", Phone: "9876500000",
	}
	assigned := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleClient, IsActive: true, AgencyID: "ADM-AAAAAA"}
	orphan := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleClient, IsActive: true}
	ghost := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleClient, IsActive: true, AgencyID: "ADM-GONE00"}
	repo := newFakeUserRepo(admin, assigned, orphan, ghost)
	svc := newTestUserService(repo)
	ctx := context.Background()

	advisor, err := svc.GetMyAdvisor(ctx, assigned.ID.Hex())
	if err != nil || advisor == nil {
		t.Fatalf("expected an advisor, got %v (%v)", advisor, err)
	}
	if advisor.Name != "Asha Nair" || advisor.Phone != "9876500000" || advisor.AgencyID != "ADM-AAAAAA" {
		t.Fatalf("unexpected advisor details: %+v", advisor)
	}

	// No agency, and an agency whose admin no longer exists, both answer "nobody yet" — not an error.
	for _, user := range []*domain.User{orphan, ghost} {
		advisor, err := svc.GetMyAdvisor(ctx, user.ID.Hex())
		if err != nil || advisor != nil {
			t.Fatalf("expected no advisor without an error, got %v (%v)", advisor, err)
		}
	}
}
