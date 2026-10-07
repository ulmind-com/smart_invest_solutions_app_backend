package service

import (
	"context"
	"testing"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// fakeClientMapRepo records the query it was handed so the tests can assert on the scoping the
// service applied, which is the part that keeps one agency's client base out of another's view.
type fakeClientMapRepo struct {
	domain.ClientMapRepository
	lastQuery *domain.ClientMapQuery
	result    *domain.ClientMapResult
	err       error
}

func (r *fakeClientMapRepo) FindClientMap(_ context.Context, query domain.ClientMapQuery) (*domain.ClientMapResult, error) {
	q := query
	r.lastQuery = &q
	if r.err != nil {
		return nil, r.err
	}
	return r.result, nil
}

func TestClientMapScopesAPlainAdminToTheirOwnAgency(t *testing.T) {
	admin := &domain.User{
		ID:      bson.NewObjectID(),
		Name:    "Asha Nair",
		Role:    domain.RoleAdmin,
		AdminID: "ADM-ASHA01",
	}
	repo := &fakeClientMapRepo{result: &domain.ClientMapResult{}}
	svc := NewClientMapService(repo, newFakeUserRepo(admin))

	// The admin asks for another agency's clients; their own agency must replace it.
	_, err := svc.GetClientMap(context.Background(), domain.RoleAdmin, admin.ID.Hex(), domain.ClientMapQuery{
		AgencyID: "ADM-SOMEONE_ELSE",
	})
	if err != nil {
		t.Fatalf("GetClientMap: %v", err)
	}
	if repo.lastQuery == nil {
		t.Fatal("expected the repository to be queried")
	}
	if repo.lastQuery.AgencyID != "ADM-ASHA01" {
		t.Errorf("agency filter = %q, want the caller's own ADM-ASHA01", repo.lastQuery.AgencyID)
	}
}

func TestClientMapFailsClosedForAnAdminWithNoAgencyID(t *testing.T) {
	// An admin account with no AdminID can't be scoped. Falling through would hand them the
	// unfiltered super-admin view, so the map must come back empty instead.
	admin := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleAdmin}
	repo := &fakeClientMapRepo{result: &domain.ClientMapResult{Total: 99}}
	svc := NewClientMapService(repo, newFakeUserRepo(admin))

	result, err := svc.GetClientMap(context.Background(), domain.RoleAdmin, admin.ID.Hex(), domain.ClientMapQuery{})
	if err != nil {
		t.Fatalf("GetClientMap: %v", err)
	}
	if repo.lastQuery != nil {
		t.Error("the repository must not be queried when the caller's agency can't be resolved")
	}
	if result.Total != 0 || len(result.Items) != 0 {
		t.Errorf("expected an empty map, got total=%d items=%d", result.Total, len(result.Items))
	}
	if result.Summary == nil {
		t.Error("summary must never be nil — the app reads its counters directly")
	}
}

func TestClientMapLetsASuperAdminAimAtAnyAgency(t *testing.T) {
	superAdmin := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleSuperAdmin, AdminID: "ADM-ROOT"}
	repo := &fakeClientMapRepo{result: &domain.ClientMapResult{}}
	svc := NewClientMapService(repo, newFakeUserRepo(superAdmin))

	for _, agency := range []string{"ADM-ASHA01", domain.ClientMapAgencyUnassigned, ""} {
		if _, err := svc.GetClientMap(context.Background(), domain.RoleSuperAdmin, superAdmin.ID.Hex(), domain.ClientMapQuery{
			AgencyID: agency,
		}); err != nil {
			t.Fatalf("GetClientMap(%q): %v", agency, err)
		}
		if repo.lastQuery.AgencyID != agency {
			t.Errorf("agency filter = %q, want %q untouched", repo.lastQuery.AgencyID, agency)
		}
	}
}

func TestClientMapRefusesANonStaffCaller(t *testing.T) {
	client := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleClient}
	repo := &fakeClientMapRepo{result: &domain.ClientMapResult{Total: 99}}
	svc := NewClientMapService(repo, newFakeUserRepo(client))

	result, err := svc.GetClientMap(context.Background(), domain.RoleClient, client.ID.Hex(), domain.ClientMapQuery{})
	if err != nil {
		t.Fatalf("GetClientMap: %v", err)
	}
	if repo.lastQuery != nil {
		t.Error("a client must never reach the cross-account query")
	}
	if len(result.Items) != 0 {
		t.Error("a client must not be handed any rows")
	}
}

func TestClientMapNormalizesFiltersAndPaging(t *testing.T) {
	superAdmin := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleSuperAdmin}
	repo := &fakeClientMapRepo{result: &domain.ClientMapResult{}}
	svc := NewClientMapService(repo, newFakeUserRepo(superAdmin))

	cases := []struct {
		name  string
		given domain.ClientMapQuery
		want  domain.ClientMapQuery
	}{
		{
			name:  "unknown filter values read as no filter, not as a filter matching nothing",
			given: domain.ClientMapQuery{AppStatus: "installed", Holdings: "some", Page: 1, Limit: 10},
			want:  domain.ClientMapQuery{AppStatus: "", Holdings: "", Page: 1, Limit: 10},
		},
		{
			name:  "known values survive, whatever case and spacing they arrive in",
			given: domain.ClientMapQuery{AppStatus: " NOT_ON_APP ", Holdings: "Without", Page: 3, Limit: 50},
			want:  domain.ClientMapQuery{AppStatus: domain.AppPresenceNotOnApp, Holdings: domain.ClientMapHoldingsWithout, Page: 3, Limit: 50},
		},
		{
			name:  "paging is clamped so a hand-written query can't ask for the whole table",
			given: domain.ClientMapQuery{Page: 0, Limit: 100000},
			want:  domain.ClientMapQuery{Page: 1, Limit: clientMapDefaultLimit},
		},
		{
			name:  "the search term is trimmed",
			given: domain.ClientMapQuery{Search: "  ria  ", Page: 1, Limit: 25},
			want:  domain.ClientMapQuery{Search: "ria", Page: 1, Limit: 25},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.GetClientMap(context.Background(), domain.RoleSuperAdmin, superAdmin.ID.Hex(), tc.given); err != nil {
				t.Fatalf("GetClientMap: %v", err)
			}
			got := *repo.lastQuery
			if got.AppStatus != tc.want.AppStatus {
				t.Errorf("app status = %q, want %q", got.AppStatus, tc.want.AppStatus)
			}
			if got.Holdings != tc.want.Holdings {
				t.Errorf("holdings = %q, want %q", got.Holdings, tc.want.Holdings)
			}
			if got.Search != tc.want.Search {
				t.Errorf("search = %q, want %q", got.Search, tc.want.Search)
			}
			if got.Page != tc.want.Page || got.Limit != tc.want.Limit {
				t.Errorf("paging = %d/%d, want %d/%d", got.Page, got.Limit, tc.want.Page, tc.want.Limit)
			}
		})
	}
}

func TestClientMapNeverReturnsNilCollections(t *testing.T) {
	// The repository can legitimately answer with nothing filled in (an empty database). The app
	// reads items and summary unconditionally, so neither may arrive as null.
	superAdmin := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleSuperAdmin}
	repo := &fakeClientMapRepo{result: &domain.ClientMapResult{}}
	svc := NewClientMapService(repo, newFakeUserRepo(superAdmin))

	result, err := svc.GetClientMap(context.Background(), domain.RoleSuperAdmin, superAdmin.ID.Hex(), domain.ClientMapQuery{})
	if err != nil {
		t.Fatalf("GetClientMap: %v", err)
	}
	if result.Items == nil {
		t.Error("items must be an empty slice, not nil")
	}
	if result.Summary == nil {
		t.Error("summary must be present, not nil")
	}
}

// fakeLoginRepo counts RecordLogin calls so the tests can prove a real sign-in is stamped and an
// impersonation is not.
type fakeLoginRepo struct {
	*fakeUserRepo
	recorded []bson.ObjectID
}

func (r *fakeLoginRepo) RecordLogin(_ context.Context, id bson.ObjectID) error {
	r.recorded = append(r.recorded, id)
	return nil
}

func (r *fakeLoginRepo) ClearFailedLogins(_ context.Context, _ bson.ObjectID) error { return nil }

func TestSignInIsStampedButImpersonationIsNot(t *testing.T) {
	password := "Str0ngPass!"
	client := &domain.User{
		ID:              bson.NewObjectID(),
		Name:            "Riya Sen",
		Email:           "riya@example.com",
		Password:        hash(t, password),
		Role:            domain.RoleClient,
		IsActive:        true,
		IsEmailVerified: true,
	}
	superAdmin := &domain.User{
		ID:       bson.NewObjectID(),
		Name:     "Root",
		Email:    "root@example.com",
		Role:     domain.RoleSuperAdmin,
		IsActive: true,
	}

	repo := &fakeLoginRepo{fakeUserRepo: newFakeUserRepo(client, superAdmin)}
	svc := newTestUserService(repo)

	if _, err := svc.Login(context.Background(), &domain.UserLoginRequest{
		Identifier: client.Email,
		Password:   password,
	}); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if len(repo.recorded) != 1 || repo.recorded[0] != client.ID {
		t.Fatalf("a successful sign-in must be stamped once on the signing-in account, got %v", repo.recorded)
	}
	if client.LastLoginAt == nil {
		t.Error("the returned account should carry the sign-in time it was just stamped with")
	}
	if client.LoginCount != 1 {
		t.Errorf("login count = %d, want 1", client.LoginCount)
	}

	// A super admin looking into the account is not that client using the app.
	before := len(repo.recorded)
	if _, err := svc.ImpersonateUser(context.Background(), superAdmin.ID.Hex(), client.ID.Hex(), "support"); err != nil {
		t.Fatalf("ImpersonateUser: %v", err)
	}
	if len(repo.recorded) != before {
		t.Errorf("impersonation must not stamp a sign-in, got %v", repo.recorded)
	}
}

func TestAWrongPasswordIsNotStampedAsASignIn(t *testing.T) {
	client := &domain.User{
		ID:              bson.NewObjectID(),
		Email:           "riya@example.com",
		Password:        hash(t, "Str0ngPass!"),
		Role:            domain.RoleClient,
		IsActive:        true,
		IsEmailVerified: true,
	}
	repo := &fakeLoginRepo{fakeUserRepo: newFakeUserRepo(client)}
	svc := newTestUserService(repo)

	if _, err := svc.Login(context.Background(), &domain.UserLoginRequest{
		Identifier: client.Email,
		Password:   "wrong-one",
	}); err == nil {
		t.Fatal("expected the wrong password to be refused")
	}
	if len(repo.recorded) != 0 {
		t.Errorf("a failed sign-in must leave no trace of app usage, got %v", repo.recorded)
	}
}

func TestAnUnapprovedAccountIsNotStampedAsASignIn(t *testing.T) {
	// The secret is right, but the account is still awaiting approval — it has not signed in, and
	// must not be counted as being on the app.
	password := "Str0ngPass!"
	client := &domain.User{
		ID:              bson.NewObjectID(),
		Email:           "pending@example.com",
		Password:        hash(t, password),
		Role:            domain.RoleClient,
		IsActive:        false,
		IsEmailVerified: true,
	}
	repo := &fakeLoginRepo{fakeUserRepo: newFakeUserRepo(client)}
	svc := newTestUserService(repo)

	if _, err := svc.Login(context.Background(), &domain.UserLoginRequest{
		Identifier: client.Email,
		Password:   password,
	}); err == nil {
		t.Fatal("expected an unapproved account to be refused")
	}
	if len(repo.recorded) != 0 {
		t.Errorf("an account that couldn't sign in must not be stamped, got %v", repo.recorded)
	}
	if client.LastLoginAt != nil {
		t.Error("an unapproved account must keep an empty sign-in time")
	}
}

// Guards the one assumption the whole map rests on: presence is only ever derived from a real
// sign-in, so the stamp is written after every check that could still refuse the login.
func TestRecordLoginRunsAfterEveryLoginCheck(t *testing.T) {
	admin := &domain.User{
		ID:              bson.NewObjectID(),
		Email:           "expired@example.com",
		AdminID:         "ADM-EXPIRED",
		Password:        hash(t, "Str0ngPass!"),
		Role:            domain.RoleAdmin,
		IsActive:        true,
		IsEmailVerified: true,
		AdminExpiryDate: ptrTime(time.Now().UTC().Add(-24 * time.Hour)),
	}
	repo := &fakeLoginRepo{fakeUserRepo: newFakeUserRepo(admin)}
	svc := newTestUserService(repo)

	if _, err := svc.AdminLogin(context.Background(), &domain.AdminLoginRequest{
		AdminID: admin.AdminID,
		PIN:     "Str0ngPass!",
	}); err == nil {
		t.Fatal("expected an expired admin account to be refused")
	}
	if len(repo.recorded) != 0 {
		t.Errorf("an expired admin never got in, so nothing may be stamped, got %v", repo.recorded)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
