package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestNormalizeAdminID(t *testing.T) {
	cases := []struct {
		name  string
		given string
		want  string
		fails bool
	}{
		{name: "already canonical", given: "ADM-ASHA01", want: "ADM-ASHA01"},
		{name: "lower case is accepted — this gets typed by hand", given: "adm-asha01", want: "ADM-ASHA01"},
		{name: "the prefix may be left off", given: "ASHA01", want: "ADM-ASHA01"},
		{name: "surrounding space is not a typo worth refusing", given: "  adm-asha01  ", want: "ADM-ASHA01"},
		{name: "spacing used when reading a code out loud", given: "ADM ASHA 01", want: "ADM-ASHA01"},
		{name: "a doubled prefix from pasting", given: "ADM-ADM-ASHA01", want: "ADM-ASHA01"},
		{name: "digits only", given: "1234", want: "ADM-1234"},
		{name: "the longest allowed body", given: "ABCDEFGHJKMN", want: "ADM-ABCDEFGHJKMN"},
		{name: "empty means generate one instead", given: "   ", want: ""},

		{name: "too short to be distinctive", given: "AB", fails: true},
		{name: "too long to read out", given: "ABCDEFGHJKMNP", fails: true},
		{name: "no punctuation — the client app would refuse it", given: "ASHA_01", fails: true},
		{name: "no spaces inside the stored value", given: "ASHA.01", fails: true},
		{name: "nothing but the prefix", given: "ADM-", fails: true},
		{name: "non-ASCII", given: "আশা01", fails: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeAdminID(tc.given)
			if tc.fails {
				if err == nil {
					t.Fatalf("normalizeAdminID(%q) = %q, want an error", tc.given, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeAdminID(%q): %v", tc.given, err)
			}
			if got != tc.want {
				t.Errorf("normalizeAdminID(%q) = %q, want %q", tc.given, got, tc.want)
			}
		})
	}
}

// Every normalized ID must satisfy the pattern the client app validates the Agency ID box against —
// an ID that slipped past this is one no client could ever type at signup.
func TestANormalizedAdminIDIsTypeableByAClient(t *testing.T) {
	for _, input := range []string{"asha01", "ADM-ASHA01", "1234", "adm abc 123", "ABCDEFGHJKMN"} {
		got, err := normalizeAdminID(input)
		if err != nil {
			t.Fatalf("normalizeAdminID(%q): %v", input, err)
		}
		// The app's rule, kept in sync deliberately: /^ADM-[A-Z0-9]{4,12}$/i on a trimmed, uppercased
		// value. A 3-character body is accepted here but shorter than the app's minimum, so the
		// generator and anything a Super Admin picks must clear the app's bar too.
		body := strings.TrimPrefix(got, "ADM-")
		if len(body) < 3 || len(body) > 12 {
			t.Errorf("normalizeAdminID(%q) = %q: body length %d is outside 3–12", input, got, len(body))
		}
		for _, r := range body {
			if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", r) {
				t.Errorf("normalizeAdminID(%q) = %q contains %q, which a client could not type", input, got, r)
			}
		}
	}
}

func newAdminCreationService(t *testing.T, seed ...*domain.User) (domain.UserService, *fakeUserRepo) {
	t.Helper()
	repo := newFakeUserRepo(seed...)
	return newTestUserService(repo), repo
}

func TestSuperAdminCanChooseAnAdminID(t *testing.T) {
	svc, repo := newAdminCreationService(t)

	created, err := svc.CreateAdmin(context.Background(), &domain.CreateAdminRequest{
		Name:       "Asha Nair",
		Email:      "asha@agency.in",
		Phone:      "9876500000",
		AdminID:    "asha01",
		ExpiryDate: time.Now().UTC().Add(365 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	if created.AdminID != "ADM-ASHA01" {
		t.Errorf("admin ID = %q, want the canonical ADM-ASHA01", created.AdminID)
	}
	if created.Admin.AdminID != "ADM-ASHA01" {
		t.Errorf("the stored account should carry the chosen ID, got %q", created.Admin.AdminID)
	}

	// The ID is also the Agency ID, so it has to be findable by the value a client would type.
	found, err := repo.FindByAdminID(context.Background(), "ADM-ASHA01")
	if err != nil || found == nil {
		t.Fatalf("the chosen Admin ID should resolve to the account: %v", err)
	}
}

func TestAnOmittedAdminIDIsGenerated(t *testing.T) {
	svc, _ := newAdminCreationService(t)

	created, err := svc.CreateAdmin(context.Background(), &domain.CreateAdminRequest{
		Name:       "Bikash Roy",
		Email:      "bikash@agency.in",
		Phone:      "9876500001",
		ExpiryDate: time.Now().UTC().Add(365 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}
	if !strings.HasPrefix(created.AdminID, "ADM-") || len(created.AdminID) != len("ADM-")+6 {
		t.Errorf("expected a generated ADM-XXXXXX, got %q", created.AdminID)
	}
}

func TestATakenAdminIDIsRefused(t *testing.T) {
	existing := &domain.User{
		ID:       bson.NewObjectID(),
		Name:     "Asha Nair",
		Email:    "asha@agency.in",
		Role:     domain.RoleAdmin,
		AdminID:  "ADM-ASHA01",
		IsActive: true,
	}
	svc, repo := newAdminCreationService(t, existing)
	before := len(repo.users)

	// Including when it is typed in a different shape than it was stored.
	for _, attempt := range []string{"ADM-ASHA01", "asha01", "adm asha 01"} {
		_, err := svc.CreateAdmin(context.Background(), &domain.CreateAdminRequest{
			Name:       "Someone Else",
			Email:      "someone@agency.in",
			Phone:      "9876500002",
			AdminID:    attempt,
			ExpiryDate: time.Now().UTC().Add(365 * 24 * time.Hour),
		})
		if err == nil {
			t.Fatalf("Admin ID %q is taken and must be refused", attempt)
		}
		if !strings.Contains(err.Error(), "already in use") {
			t.Errorf("the error should say the ID is taken, got %v", err)
		}
	}
	if len(repo.users) != before {
		t.Error("a refused Admin ID must not create an account")
	}
}

func TestAMalformedAdminIDIsRefusedBeforeAnythingIsCreated(t *testing.T) {
	svc, repo := newAdminCreationService(t)
	before := len(repo.users)

	_, err := svc.CreateAdmin(context.Background(), &domain.CreateAdminRequest{
		Name:       "Asha Nair",
		Email:      "asha@agency.in",
		Phone:      "9876500000",
		AdminID:    "no!",
		ExpiryDate: time.Now().UTC().Add(365 * 24 * time.Hour),
	})
	if err == nil {
		t.Fatal("a malformed Admin ID must be refused")
	}
	if len(repo.users) != before {
		t.Error("nothing may be created when the Admin ID is rejected")
	}
}

func TestSuggestAdminIDReturnsSomethingFree(t *testing.T) {
	svc, repo := newAdminCreationService(t)

	suggestion, err := svc.SuggestAdminID(context.Background())
	if err != nil {
		t.Fatalf("SuggestAdminID: %v", err)
	}
	if !strings.HasPrefix(suggestion.AdminID, "ADM-") {
		t.Errorf("expected an ADM- prefixed ID, got %q", suggestion.AdminID)
	}
	if taken, _ := repo.FindByAdminID(context.Background(), suggestion.AdminID); taken != nil {
		t.Error("a suggested ID must not already belong to an account")
	}

	// It reserves nothing, so it must still be usable as the chosen ID.
	if _, err := svc.CreateAdmin(context.Background(), &domain.CreateAdminRequest{
		Name:       "Asha Nair",
		Email:      "asha@agency.in",
		Phone:      "9876500000",
		AdminID:    suggestion.AdminID,
		ExpiryDate: time.Now().UTC().Add(365 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("a suggested ID should be accepted as the chosen one: %v", err)
	}
}

// A chosen Admin ID is also the Agency ID clients sign up with, so it has to work end to end: a
// client typing it lands in that agency and credits that admin.
func TestAChosenAdminIDWorksAsAnAgencyID(t *testing.T) {
	svc, repo := newAdminCreationService(t)
	ctx := context.Background()

	created, err := svc.CreateAdmin(ctx, &domain.CreateAdminRequest{
		Name:       "Asha Nair",
		Email:      "asha@agency.in",
		Phone:      "9876500000",
		AdminID:    "ASHA01",
		ExpiryDate: time.Now().UTC().Add(365 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateAdmin: %v", err)
	}

	refs := &fakeReferralRepo{}
	if setter, ok := svc.(interface {
		SetReferralRepository(domain.ReferralRepository)
	}); ok {
		setter.SetReferralRepository(refs)
	}

	// Typed in lower case, the way a client would.
	client, err := svc.Register(ctx, &domain.CreateUserRequest{
		Name: "Riya Sen", Email: "riya@example.com", Password: "secret1", AgencyID: "adm-asha01",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if client.AgencyID != "ADM-ASHA01" {
		t.Errorf("the client should be filed under ADM-ASHA01, got %q", client.AgencyID)
	}
	if len(refs.records) != 1 || refs.records[0].ReferrerID != created.Admin.ID {
		t.Errorf("the signup should credit the chosen admin, got %+v", refs.records)
	}
	if _, err := repo.FindByAdminID(ctx, "ADM-ASHA01"); err != nil {
		t.Errorf("the agency lookup should resolve: %v", err)
	}
}
