package middleware

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/utils"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// accountCacheTTL bounds how long a looked-up account is reused before the database is consulted
// again. It is the longest a deactivation, merge, deletion or role change can take to lock out a
// session that is already signed in, and it keeps the extra lookup off most requests.
const accountCacheTTL = 20 * time.Second

type cachedAccount struct {
	user      *domain.User // nil means "no such account"
	fetchedAt time.Time
}

// accountGuard re-validates the account behind an otherwise valid JWT.
type accountGuard struct {
	users domain.UserRepository
	mu    sync.Mutex
	cache map[bson.ObjectID]cachedAccount
}

func newAccountGuard(users domain.UserRepository) *accountGuard {
	return &accountGuard{users: users, cache: make(map[bson.ObjectID]cachedAccount)}
}

// load returns the account, or (nil, nil) when it no longer exists. A non-nil error means the
// lookup itself failed (database unreachable) and says nothing about the account.
func (g *accountGuard) load(ctx context.Context, id bson.ObjectID) (*domain.User, error) {
	now := time.Now()

	g.mu.Lock()
	if entry, ok := g.cache[id]; ok && now.Sub(entry.fetchedAt) < accountCacheTTL {
		g.mu.Unlock()
		return entry.user, nil
	}
	g.mu.Unlock()

	user, err := g.users.FindByID(ctx, id)
	if err != nil {
		if !errors.Is(err, domain.ErrUserNotFound) {
			return nil, err
		}
		user = nil
	}

	g.mu.Lock()
	// Opportunistic sweep so the map can't grow without bound on a long-running instance.
	if len(g.cache) > 10000 {
		for key, entry := range g.cache {
			if now.Sub(entry.fetchedAt) >= accountCacheTTL {
				delete(g.cache, key)
			}
		}
	}
	g.cache[id] = cachedAccount{user: user, fetchedAt: now}
	g.mu.Unlock()

	return user, nil
}

// check returns (0, "") when the request may proceed, or the HTTP status and message to reject it
// with. Every "this session is over" case answers 401, which the app treats as "sign in again".
func (g *accountGuard) check(ctx context.Context, claims *utils.Claims) (int, string) {
	user, err := g.load(ctx, claims.UserID)
	if err != nil {
		log.Error().Err(err).Str("user_id", claims.UserID.Hex()).Msg("account guard lookup failed")
		return http.StatusServiceUnavailable, "We couldn't verify your session right now. Please try again in a moment."
	}
	if msg := AccountRejection(user, claims.Role, time.Now().UTC()); msg != "" {
		return http.StatusUnauthorized, msg
	}

	// An impersonation session is only as good as the super admin who opened it.
	if claims.ImpersonatedBy != nil {
		impersonator, err := g.load(ctx, *claims.ImpersonatedBy)
		if err != nil {
			log.Error().Err(err).Msg("account guard impersonator lookup failed")
			return http.StatusServiceUnavailable, "We couldn't verify your session right now. Please try again in a moment."
		}
		if msg := AccountRejection(impersonator, domain.RoleSuperAdmin, time.Now().UTC()); msg != "" {
			return http.StatusUnauthorized, "The super admin session behind this view has ended. Please sign in again."
		}
	}

	return 0, ""
}

// AccountRejection explains why a token for this account must no longer be honoured, or returns ""
// when it is fine. tokenRole is the role the token was issued with.
func AccountRejection(user *domain.User, tokenRole string, now time.Time) string {
	switch {
	case user == nil:
		return "This account no longer exists. Please sign in again."
	case user.MergedIntoUserID != nil:
		return "This account has been merged into another family account. Please sign in with that account instead."
	case !user.IsActive:
		return "Your account has been deactivated. Please contact your advisor."
	case user.Role == domain.RoleAdmin && user.AdminExpiryDate != nil && user.AdminExpiryDate.Before(now):
		return "Your admin access has expired. Please contact your super admin to renew it."
	case user.Role != tokenRole:
		return "Your access level has changed. Please sign in again."
	}
	return ""
}
