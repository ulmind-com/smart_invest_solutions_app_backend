package middleware

import (
	"testing"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestAccountRejection(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	merged := bson.NewObjectID()

	cases := []struct {
		name      string
		user      *domain.User
		tokenRole string
		rejected  bool
	}{
		{"active client", &domain.User{Role: domain.RoleClient, IsActive: true}, domain.RoleClient, false},
		{"deleted", nil, domain.RoleClient, true},
		{"deactivated", &domain.User{Role: domain.RoleClient, IsActive: false}, domain.RoleClient, true},
		{"merged", &domain.User{Role: domain.RoleClient, IsActive: true, MergedIntoUserID: &merged}, domain.RoleClient, true},
		{"expired admin", &domain.User{Role: domain.RoleAdmin, IsActive: true, AdminExpiryDate: &past}, domain.RoleAdmin, true},
		{"valid admin", &domain.User{Role: domain.RoleAdmin, IsActive: true, AdminExpiryDate: &future}, domain.RoleAdmin, false},
		{"role changed", &domain.User{Role: domain.RoleAdvisor, IsActive: true}, domain.RoleClient, true},
	}
	for _, tc := range cases {
		if got := AccountRejection(tc.user, tc.tokenRole, now) != ""; got != tc.rejected {
			t.Errorf("%s: rejected=%v, want %v", tc.name, got, tc.rejected)
		}
	}
}
