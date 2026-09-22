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

// referralOwner resolves a typed referral code to the staff member who owns it. It returns nil for
// an empty or unknown code, and for a code that belongs to a non-staff account — referral codes are
// issued to admins only, and legacy client codes (from when clients could refer) no longer count.
func referralOwner(ctx context.Context, userRepo domain.UserRepository, referralCode string) *domain.User {
	code := normalizeReferralCode(referralCode)
	if code == "" {
		return nil
	}
	owner, err := userRepo.FindByReferralCode(ctx, code)
	if err != nil || owner == nil {
		return nil
	}
	if owner.Role != domain.RoleAdmin && owner.Role != domain.RoleSuperAdmin {
		return nil
	}
	// A retired or deactivated staff account must not keep onboarding clients: nobody would be able
	// to sign in and manage them, and the agency link would point at a dead account.
	if !owner.IsActive || owner.MergedIntoUserID != nil {
		return nil
	}
	return owner
}

// resolveOnboardingAgency decides which agency a new signup belongs to. An explicitly typed Agency
// ID always wins (and must be valid). Otherwise a valid advisor referral code decides it, so an
// admin only ever has to share one code: it both credits them for the referral and files the client
// under their agency.
func resolveOnboardingAgency(ctx context.Context, userRepo domain.UserRepository, rawAgencyID, referralCode string) (string, error) {
	if strings.TrimSpace(rawAgencyID) != "" {
		return resolveAgencyAdminID(ctx, userRepo, rawAgencyID)
	}
	if owner := referralOwner(ctx, userRepo, referralCode); owner != nil {
		return owner.AdminID, nil
	}
	return "", nil
}

// recordPendingReferral files a Pending referral against the admin who owns referralCode, so they
// are credited once that person's account is approved. It is a no-op for an empty or unknown code,
// a code that isn't a staff member's, a self-referral, or an email that already has a pending
// referral — a referral problem must never block someone from signing up.
func recordPendingReferral(ctx context.Context, referralRepo domain.ReferralRepository, userRepo domain.UserRepository, referralCode, referredEmail, referredName, referredPhone string) {
	email := utils.NormalizeEmail(referredEmail)
	if email == "" || referralRepo == nil {
		return
	}

	referrer := referralOwner(ctx, userRepo, referralCode)
	if referrer == nil || utils.NormalizeEmail(referrer.Email) == email {
		return
	}

	if existing, _ := referralRepo.GetPendingByReferredEmail(ctx, email); existing != nil {
		return
	}

	if _, err := referralRepo.Create(ctx, &domain.ReferralRecord{
		ReferrerID:    referrer.ID,
		ReferredEmail: email,
		ReferredName:  strings.TrimSpace(referredName),
		ReferredPhone: strings.TrimSpace(referredPhone),
		Status:        domain.ReferralStatusPending,
	}); err != nil {
		log.Error().Err(err).Str("email", email).Msg("failed to record referral")
	}
}

// completeReferral converts the pending referral for an approved client, if there is one, and
// stamps the account it produced. Nothing about it can fail the approval it follows.
func completeReferral(ctx context.Context, referralRepo domain.ReferralRepository, email string, user *domain.UserResponse) {
	if referralRepo == nil || user == nil {
		return
	}
	pending, err := referralRepo.GetPendingByReferredEmail(ctx, email)
	if err != nil || pending == nil {
		return
	}
	if err := referralRepo.Complete(ctx, pending.ID, user.ID, user.Name, user.Phone); err != nil {
		log.Error().Err(err).Str("email", email).Msg("failed to complete referral")
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
