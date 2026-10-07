package service

import (
	"context"
	"fmt"
	"regexp"
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

// adminIDPattern is the shape of an Admin ID's body — the part after the "ADM-" prefix. Letters and
// digits only, 3 to 12 of them, so an ID stays short enough to read out over the phone and long
// enough to be distinctive.
var adminIDPattern = regexp.MustCompile(`^[A-Z0-9]{3,12}$`)

// adminIDPrefix is carried by every Admin ID on the platform, generated or hand-picked, so one
// canonical shape reaches the client app — which validates the Agency ID a client types against it.
const adminIDPrefix = "ADM-"

// normalizeAdminID canonicalises an Admin ID a Super Admin typed by hand.
//
// It is forgiving about how the ID is entered — case is ignored, surrounding space and inner spaces
// or dashes are dropped, and the "ADM-" prefix may be left off — because this value gets read out
// over the phone and typed back by clients. What it will not do is accept a shape the client app
// would later refuse: the Agency ID box on the signup form validates against this same prefix and
// character set, so an ID that slipped through here would be one no client could ever type.
//
// An empty input returns "" with no error, meaning "generate one instead".
func normalizeAdminID(raw string) (string, error) {
	cleaned := strings.ToUpper(strings.TrimSpace(raw))
	// Spaces and dashes inside the body are how people naturally break up a code they are reading
	// out ("ADM ASHA 01"); they carry no meaning, so they go rather than becoming an error.
	cleaned = strings.NewReplacer(" ", "", "\t", "").Replace(cleaned)
	if cleaned == "" {
		return "", nil
	}

	body := strings.TrimPrefix(cleaned, adminIDPrefix)
	// A stray "ADM" with no dash, or a doubled prefix from pasting, still means the same thing.
	body = strings.TrimPrefix(body, "ADM")
	body = strings.TrimLeft(body, "-")
	body = strings.ReplaceAll(body, "-", "")

	if !adminIDPattern.MatchString(body) {
		return "", fmt.Errorf("an Admin ID must be 3–12 letters or digits, optionally after %q — for example %sASHA01", adminIDPrefix, adminIDPrefix)
	}

	return adminIDPrefix + body, nil
}

// agencyOwner resolves an Agency ID / Admin ID to the staff account behind it, or nil when it names
// nobody usable.
//
// A retired or deactivated staff account must not keep onboarding clients: nobody would be able to
// sign in and manage them, and the agency link would point at a dead account.
func agencyOwner(ctx context.Context, userRepo domain.UserRepository, agencyID string) *domain.User {
	id := strings.ToUpper(strings.TrimSpace(agencyID))
	if id == "" {
		return nil
	}
	owner, err := userRepo.FindByAdminID(ctx, id)
	if err != nil || owner == nil {
		return nil
	}
	if owner.Role != domain.RoleAdmin && owner.Role != domain.RoleSuperAdmin {
		return nil
	}
	if !owner.IsActive || owner.MergedIntoUserID != nil {
		return nil
	}
	return owner
}

// recordPendingReferral credits the admin whose Agency ID a new applicant signed up with, so the
// super admin's report can show which admin brought in which clients.
//
// There is one code on this platform, not two: an admin's Admin ID *is* their Agency ID, and sharing
// it both files the client under that agency and credits the admin for bringing them in. A separate
// referral code only ever duplicated that, and left an applicant wondering which of the two boxes
// mattered.
//
// It is a no-op for an empty or unknown Agency ID, an agency that is somehow the applicant's own
// account, or an email that already has a pending referral — a bookkeeping problem must never block
// someone from signing up.
func recordPendingReferral(ctx context.Context, referralRepo domain.ReferralRepository, userRepo domain.UserRepository, agencyID, referredEmail, referredName, referredPhone string) {
	email := utils.NormalizeEmail(referredEmail)
	if email == "" || referralRepo == nil {
		return
	}

	referrer := agencyOwner(ctx, userRepo, agencyID)
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

// ensureClientMayModify refuses a client's edit or delete of a record their agency maintains.
// The client app shows those as "Managed by advisor"; letting the client change or remove one
// anyway would silently desync the agency's book from what the client sees.
func ensureClientMayModify(requesterRole, managedBy string) error {
	if isAgencyStaff(requesterRole) {
		return nil
	}
	if domain.NormalizeManagedBy(managedBy) == domain.ManagedByAgency {
		return fmt.Errorf("your advisor manages this record — ask them to update or remove it")
	}
	return nil
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
