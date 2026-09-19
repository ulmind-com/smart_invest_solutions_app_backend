package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/utils"
)

// resolveAgencyAdminID validates an Agency ID a prospective client typed in (the AdminID of the
// admin they're signing up under) and returns its canonical form. An empty input is allowed and
// resolves to "" (unassigned — visible to a super admin only). Shared by the access-request form and
// the self-service signup so both onboarding doors accept exactly the same IDs.
func resolveAgencyAdminID(ctx context.Context, userRepo domain.UserRepository, rawAgencyID string) (string, error) {
	trimmed := strings.ToUpper(strings.TrimSpace(rawAgencyID))
	if trimmed == "" {
		return "", nil
	}

	owner, err := userRepo.FindByAdminID(ctx, trimmed)
	if err != nil || owner == nil || (owner.Role != domain.RoleAdmin && owner.Role != domain.RoleSuperAdmin) {
		return "", fmt.Errorf("invalid Agency ID — please double-check it with your agency and try again")
	}

	return owner.AdminID, nil
}

// normalizeReferralCode canonicalises a typed referral code (codes are generated upper-case).
func normalizeReferralCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// recordPendingReferral files a Pending referral lead for referredEmail against the owner of
// referralCode, so the referrer is credited once that person's account is approved. It is a no-op
// for an empty or unknown code, a self-referral, or an email that already has a pending lead —
// referral problems must never block someone from signing up.
func recordPendingReferral(ctx context.Context, referralRepo domain.ReferralRepository, userRepo domain.UserRepository, referralCode, referredEmail string) {
	code := normalizeReferralCode(referralCode)
	email := utils.NormalizeEmail(referredEmail)
	if code == "" || email == "" || referralRepo == nil {
		return
	}

	referrer, _ := userRepo.FindByReferralCode(ctx, code)
	if referrer == nil || utils.NormalizeEmail(referrer.Email) == email {
		return
	}

	if existing, _ := referralRepo.GetPendingByReferredEmail(ctx, email); existing != nil {
		return
	}

	if _, err := referralRepo.Create(ctx, &domain.ReferralRecord{
		ReferrerID:    referrer.ID,
		ReferredEmail: email,
		Status:        domain.ReferralStatusPending,
	}); err != nil {
		log.Error().Err(err).Str("email", email).Msg("failed to record referral lead")
	}
}

// generateUniqueReferralCode returns a 6-character referral code no other account holds, falling
// back to a timestamp-derived code in the (astronomically unlikely) event every attempt collides.
func generateUniqueReferralCode(ctx context.Context, userRepo domain.UserRepository) string {
	for attempt := 0; attempt < 5; attempt++ {
		candidate, _ := utils.GenerateReferralCode(6)
		if candidate == "" {
			continue
		}
		if existing, _ := userRepo.FindByReferralCode(ctx, candidate); existing == nil {
			return candidate
		}
	}
	return "REF" + strings.ToUpper(strconv.FormatInt(time.Now().UnixNano(), 36))
}

// isAgencyStaff reports whether role may set agency-only flags such as is_mapped.
func isAgencyStaff(role string) bool {
	return role == domain.RoleAdmin || role == domain.RoleSuperAdmin
}

// normalizeISODate validates a calendar date sent as YYYY-MM-DD (the format motor-policy expiry and
// family-member birth dates are stored in — dashboards and reports parse it back with that layout)
// and returns it in canonical form. A full RFC3339 timestamp is accepted and reduced to its date.
func normalizeISODate(value, field string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if t, err := time.Parse("2006-01-02", trimmed); err == nil {
		return t.Format("2006-01-02"), nil
	}
	if t, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return t.Format("2006-01-02"), nil
	}
	return "", fmt.Errorf("%s must be a valid date in YYYY-MM-DD format", field)
}
