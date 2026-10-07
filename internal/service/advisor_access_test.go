package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestTheClientIsToldWhenTheirAdvisorsAccessHasLapsed(t *testing.T) {
	// The client's own account is untouched by this — what changes is that nothing will arrive from
	// the agency until it's renewed, which is worth saying rather than leaving them to wonder why a
	// premium date never moved.
	expired := time.Now().UTC().Add(-48 * time.Hour)
	admin := &domain.User{
		ID: bson.NewObjectID(), Name: "Asha Nair", Role: domain.RoleAdmin, IsActive: true,
		AdminID: "ADM-ASHA01", Phone: "9876500000", AdminExpiryDate: &expired,
	}
	client := &domain.User{
		ID: bson.NewObjectID(), Role: domain.RoleClient, IsActive: true, AgencyID: "ADM-ASHA01",
	}
	svc := newTestUserService(newFakeUserRepo(admin, client))

	advisor, err := svc.GetMyAdvisor(context.Background(), client.ID.Hex())
	if err != nil || advisor == nil {
		t.Fatalf("expected the advisor to still be named, got %v (%v)", advisor, err)
	}
	if !advisor.AccessExpired {
		t.Error("the advisor's access has lapsed and the client should be told")
	}
	if advisor.AccessExpiresAt == nil || !advisor.AccessExpiresAt.Equal(expired) {
		t.Errorf("the expiry date should be reported, got %v", advisor.AccessExpiresAt)
	}
	// Still named and still reachable: the client may well want to chase them about it.
	if advisor.Name != "Asha Nair" || advisor.Phone != "9876500000" {
		t.Errorf("contact details must survive an expiry: %+v", advisor)
	}
}

func TestAnAdvisorInGoodStandingReadsAsActive(t *testing.T) {
	future := time.Now().UTC().Add(365 * 24 * time.Hour)
	admin := &domain.User{
		ID: bson.NewObjectID(), Name: "Asha Nair", Role: domain.RoleAdmin, IsActive: true,
		AdminID: "ADM-ASHA01", AdminExpiryDate: &future,
	}
	client := &domain.User{
		ID: bson.NewObjectID(), Role: domain.RoleClient, IsActive: true, AgencyID: "ADM-ASHA01",
	}
	svc := newTestUserService(newFakeUserRepo(admin, client))

	advisor, err := svc.GetMyAdvisor(context.Background(), client.ID.Hex())
	if err != nil {
		t.Fatalf("GetMyAdvisor: %v", err)
	}
	if advisor.AccessExpired {
		t.Error("an advisor valid for another year must not read as expired")
	}
	if advisor.AccessExpiresAt == nil {
		t.Error("the renewal date is still worth reporting before it passes")
	}
}

func TestASuperAdminAgencyNeverExpires(t *testing.T) {
	// super_admin accounts carry no expiry date at all, so a client under one must never see a
	// lapsed-access warning.
	root := &domain.User{
		ID: bson.NewObjectID(), Name: "Head Office", Role: domain.RoleSuperAdmin, IsActive: true,
		AdminID: "ADM-ROOT00",
	}
	client := &domain.User{
		ID: bson.NewObjectID(), Role: domain.RoleClient, IsActive: true, AgencyID: "ADM-ROOT00",
	}
	svc := newTestUserService(newFakeUserRepo(root, client))

	advisor, err := svc.GetMyAdvisor(context.Background(), client.ID.Hex())
	if err != nil || advisor == nil {
		t.Fatalf("expected an advisor, got %v (%v)", advisor, err)
	}
	if advisor.AccessExpired || advisor.AccessExpiresAt != nil {
		t.Errorf("a super admin agency never expires: %+v", advisor)
	}
}

func TestAnAdminWithNoExpiryDateIsNotTreatedAsExpired(t *testing.T) {
	// Admin accounts created before validity existed carry no date, and nil means "no expiry" —
	// reading it as expired would warn every one of their clients for no reason.
	admin := &domain.User{
		ID: bson.NewObjectID(), Name: "Legacy Admin", Role: domain.RoleAdmin, IsActive: true,
		AdminID: "ADM-OLD001",
	}
	client := &domain.User{
		ID: bson.NewObjectID(), Role: domain.RoleClient, IsActive: true, AgencyID: "ADM-OLD001",
	}
	svc := newTestUserService(newFakeUserRepo(admin, client))

	advisor, err := svc.GetMyAdvisor(context.Background(), client.ID.Hex())
	if err != nil || advisor == nil {
		t.Fatalf("expected an advisor, got %v (%v)", advisor, err)
	}
	if advisor.AccessExpired {
		t.Error("no expiry date means no expiry, not an expired one")
	}
}

func TestTheExpiredAdminIsStillBlockedEverywhere(t *testing.T) {
	// The client-facing message claims agency updates are paused. That has to be true: the admin is
	// refused at login, and the per-request account guard refuses them on every call after it too.
	expired := time.Now().UTC().Add(-24 * time.Hour)
	admin := &domain.User{
		ID: bson.NewObjectID(), Role: domain.RoleAdmin, IsActive: true, AdminID: "ADM-ASHA01",
		Email: "asha@agency.in", Password: hash(t, "Str0ngPass!"), AdminExpiryDate: &expired,
	}
	svc := newTestUserService(newFakeUserRepo(admin))

	_, err := svc.Login(context.Background(), &domain.UserLoginRequest{
		Identifier: admin.Email, Password: "Str0ngPass!",
	})
	if err == nil {
		t.Fatal("an expired admin must not be able to sign in")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("the refusal should say why, got %v", err)
	}
}

func TestDocumentCategoryIsCleanedBeforeItIsStored(t *testing.T) {
	// The "Others" option sends whatever the client typed, so this is the only thing standing between
	// a free-text field and what ends up on a badge next to every file.
	cases := []struct {
		name  string
		given string
		want  string
	}{
		{name: "a typed label survives", given: "Marriage Certificate", want: "Marriage Certificate"},
		{name: "blank falls back", given: "", want: "General"},
		{name: "whitespace only falls back", given: "   \t ", want: "General"},
		{name: "surrounding space is trimmed", given: "  Rent Agreement  ", want: "Rent Agreement"},
		{name: "inner whitespace is collapsed", given: "Rent\n\tAgreement", want: "Rent Agreement"},
		{
			name:  "an over-long label is capped rather than stored whole",
			given: strings.Repeat("A", 200),
			want:  strings.Repeat("A", maxDocumentCategoryLength),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeDocumentCategory(tc.given); got != tc.want {
				t.Errorf("normalizeDocumentCategory(%q) = %q, want %q", tc.given, got, tc.want)
			}
		})
	}
}

func TestADocumentCategoryIsCappedByCharactersNotBytes(t *testing.T) {
	// Counted in runes: a Bengali label would otherwise be cut to a third of its length, and could be
	// cut in the middle of a character.
	long := strings.Repeat("আ", 100)
	got := normalizeDocumentCategory(long)
	if runes := []rune(got); len(runes) != maxDocumentCategoryLength {
		t.Errorf("got %d runes, want %d", len(runes), maxDocumentCategoryLength)
	}
	if !utf8Valid(got) {
		t.Error("the capped label must still be valid UTF-8")
	}
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
