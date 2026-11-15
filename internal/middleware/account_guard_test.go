package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/utils"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestAccountRejection(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	merged := bson.NewObjectID()

	cases := []struct {
		name         string
		user         *domain.User
		tokenRole    string
		tokenVersion int
		rejected     bool
	}{
		{"active client", &domain.User{Role: domain.RoleClient, IsActive: true, TokenVersion: 0}, domain.RoleClient, 0, false},
		{"deleted", nil, domain.RoleClient, 0, true},
		{"deactivated", &domain.User{Role: domain.RoleClient, IsActive: false}, domain.RoleClient, 0, true},
		{"merged", &domain.User{Role: domain.RoleClient, IsActive: true, MergedIntoUserID: &merged}, domain.RoleClient, 0, true},
		{"expired admin", &domain.User{Role: domain.RoleAdmin, IsActive: true, AdminExpiryDate: &past}, domain.RoleAdmin, 0, true},
		{"valid admin", &domain.User{Role: domain.RoleAdmin, IsActive: true, AdminExpiryDate: &future}, domain.RoleAdmin, 0, false},
		{"role changed", &domain.User{Role: domain.RoleAdvisor, IsActive: true}, domain.RoleClient, 0, true},
		{"token version match", &domain.User{Role: domain.RoleClient, IsActive: true, TokenVersion: 3}, domain.RoleClient, 3, false},
		{"revoked old token version", &domain.User{Role: domain.RoleClient, IsActive: true, TokenVersion: 3}, domain.RoleClient, 2, true},
	}
	for _, tc := range cases {
		if got := AccountRejection(tc.user, tc.tokenRole, tc.tokenVersion, now) != ""; got != tc.rejected {
			t.Errorf("%s: rejected=%v, want %v", tc.name, got, tc.rejected)
		}
	}
}

type fakeGuardUserRepo struct {
	domain.UserRepository
	user *domain.User
}

func (f *fakeGuardUserRepo) FindByID(_ context.Context, id bson.ObjectID) (*domain.User, error) {
	if f.user == nil || f.user.ID != id {
		return nil, domain.ErrUserNotFound
	}
	return f.user, nil
}

func TestAccountGuardTokenRevocation(t *testing.T) {
	userID := bson.NewObjectID()
	user := &domain.User{
		ID:           userID,
		Role:         domain.RoleClient,
		IsActive:     true,
		TokenVersion: 1,
	}
	repo := &fakeGuardUserRepo{user: user}
	guard := newAccountGuard(repo)

	// Valid claim with token version 1
	claims := &utils.Claims{
		UserID:       userID,
		Role:         domain.RoleClient,
		TokenVersion: 1,
	}
	if status, msg := guard.check(context.Background(), claims); status != 0 {
		t.Fatalf("expected allowed session, got status %d: %s", status, msg)
	}

	// User logs out or changes password -> TokenVersion bumped to 2
	user.TokenVersion = 2
	InvalidateAccount(userID)

	// Old token (with version 1) is now rejected with 401
	if status, msg := guard.check(context.Background(), claims); status != 401 {
		t.Fatalf("expected status 401 for revoked token, got %d (msg: %s)", status, msg)
	}

	// New token with version 2 is allowed
	newClaims := &utils.Claims{
		UserID:       userID,
		Role:         domain.RoleClient,
		TokenVersion: 2,
	}
	if status, msg := guard.check(context.Background(), newClaims); status != 0 {
		t.Fatalf("expected allowed session for new token, got status %d: %s", status, msg)
	}
}
