package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// fakeResetRepo is an in-memory PasswordResetRepository. Embedding the interface means a method a
// test doesn't exercise panics loudly instead of needing a stub.
type fakeResetRepo struct {
	domain.PasswordResetRepository
	created []*domain.PasswordReset
	latest  *domain.PasswordReset
}

func (r *fakeResetRepo) FindLatestActiveOTP(_ context.Context, _ string) (*domain.PasswordReset, error) {
	return r.latest, nil
}

func (r *fakeResetRepo) DeleteAllByEmail(_ context.Context, _ string) error { return nil }

func (r *fakeResetRepo) Create(_ context.Context, reset *domain.PasswordReset) (*domain.PasswordReset, error) {
	reset.ID = bson.NewObjectID()
	r.created = append(r.created, reset)
	return reset, nil
}

func newResetService(t *testing.T, users ...*domain.User) (domain.PasswordResetService, *fakeResetRepo) {
	t.Helper()
	repo := &fakeResetRepo{}
	// No email service: sending is fire-and-forget in a goroutine, and these tests are about what the
	// caller is told, not about delivery.
	return NewPasswordResetService(repo, newFakeUserRepo(users...), nil), repo
}

func TestForgotPasswordSaysWhenNoAccountIsRegistered(t *testing.T) {
	// The whole point: somebody who mistyped their email is told, instead of waiting for a code that
	// was never going to arrive.
	svc, repo := newResetService(t, &domain.User{
		ID: bson.NewObjectID(), Email: "riya@example.com", Role: domain.RoleClient,
		IsActive: true, IsEmailVerified: true,
	})

	err := svc.SendOTP(context.Background(), &domain.ForgotPasswordRequest{Email: "nobody@example.com"})
	var unknown *domain.UnknownAccountError
	if !errors.As(err, &unknown) {
		t.Fatalf("expected an unknown-account error, got %v", err)
	}
	if !strings.Contains(unknown.Message, "nobody@example.com") {
		t.Errorf("the message should name the address that was tried, got %q", unknown.Message)
	}
	if len(repo.created) != 0 {
		t.Error("no OTP may be stored for an address with no account")
	}
}

func TestForgotPasswordSendsACodeForARealAccount(t *testing.T) {
	svc, repo := newResetService(t, &domain.User{
		ID: bson.NewObjectID(), Email: "riya@example.com", Role: domain.RoleClient,
		IsActive: true, IsEmailVerified: true,
	})

	// Normalised on the way in: the address is matched however it was typed.
	if err := svc.SendOTP(context.Background(), &domain.ForgotPasswordRequest{Email: "  Riya@Example.com "}); err != nil {
		t.Fatalf("SendOTP: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected one OTP to be stored, got %d", len(repo.created))
	}
	if repo.created[0].Email != "riya@example.com" {
		t.Errorf("the OTP should be keyed by the normalised address, got %q", repo.created[0].Email)
	}
	if len(repo.created[0].OTP) != 6 {
		t.Errorf("expected a 6-digit code, got %q", repo.created[0].OTP)
	}
}

func TestForgotPasswordRefusesAnAccountAResetCannotHelp(t *testing.T) {
	mergedInto := bson.NewObjectID()
	merged := &domain.User{
		ID: bson.NewObjectID(), Email: "merged@example.com", Role: domain.RoleClient,
		IsActive: true, IsEmailVerified: true, MergedIntoUserID: &mergedInto,
	}
	unverified := &domain.User{
		ID: bson.NewObjectID(), Email: "pending@example.com", Role: domain.RoleClient,
		IsActive: false, IsEmailVerified: false,
	}
	svc, repo := newResetService(t, merged, unverified)
	ctx := context.Background()

	cases := []struct {
		email string
		says  string
	}{
		// A retired login can never sign in again, whatever its password is.
		{merged.Email, "merged into another family account"},
		// Login refuses an unverified client whatever the password is, so a reset is a dead end —
		// finishing signup is what actually unblocks them.
		{unverified.Email, "hasn't been verified"},
	}

	for _, tc := range cases {
		err := svc.SendOTP(ctx, &domain.ForgotPasswordRequest{Email: tc.email})
		var unavailable *domain.ResetUnavailableError
		if !errors.As(err, &unavailable) {
			t.Fatalf("%s: expected a reset-unavailable error, got %v", tc.email, err)
		}
		if !strings.Contains(unavailable.Message, tc.says) {
			t.Errorf("%s: message should explain the problem, got %q", tc.email, unavailable.Message)
		}
	}

	if len(repo.created) != 0 {
		t.Error("no OTP may be stored for an account a reset cannot help")
	}
}

func TestForgotPasswordStillResetsAnAdminAwaitingNothing(t *testing.T) {
	// Admin accounts are created without the email-verified flag, so the unverified guard must not
	// catch them — it is a client-signup rule, and an admin locked out needs this to work.
	admin := &domain.User{
		ID: bson.NewObjectID(), Email: "asha@agency.in", Role: domain.RoleAdmin,
		AdminID: "ADM-ASHA01", IsActive: true, IsEmailVerified: false,
	}
	svc, repo := newResetService(t, admin)

	if err := svc.SendOTP(context.Background(), &domain.ForgotPasswordRequest{Email: admin.Email}); err != nil {
		t.Fatalf("an admin must still be able to reset their password: %v", err)
	}
	if len(repo.created) != 1 {
		t.Errorf("expected a code to be stored for the admin, got %d", len(repo.created))
	}
}

func TestForgotPasswordRejectsABlankAddress(t *testing.T) {
	svc, repo := newResetService(t)

	err := svc.SendOTP(context.Background(), &domain.ForgotPasswordRequest{Email: "   "})
	var unknown *domain.UnknownAccountError
	if !errors.As(err, &unknown) {
		t.Fatalf("expected a blank address to be refused, got %v", err)
	}
	if len(repo.created) != 0 {
		t.Error("nothing may be stored for a blank address")
	}
}
