package domain

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// ErrUserNotFound is returned by UserRepository lookups when no account matches. Its message is
// kept identical to the old ad-hoc string so API responses are unchanged, while callers (e.g. the
// auth middleware) can tell "the account is gone" apart from "the database is unreachable".
var ErrUserNotFound = errors.New("user not found")

// Role constants
const (
	RoleClient     = "client"
	RoleAdvisor    = "advisor"
	RoleAdmin      = "admin"
	RoleSuperAdmin = "super_admin"
)

// User represents a user entity in the system.
type User struct {
	ID              bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Name            string        `bson:"name" json:"name" binding:"required"`
	Email           string        `bson:"email" json:"email" binding:"required,email"`
	Password        string        `bson:"password" json:"-"`
	Phone           string        `bson:"phone,omitempty" json:"phone,omitempty"`
	Role            string        `bson:"role" json:"role"` // client, advisor, admin, super_admin
	IsActive        bool          `bson:"is_active" json:"is_active"`
	IsEmailVerified bool          `bson:"is_email_verified" json:"is_email_verified"`
	// AdminID is the account's unique login ID, set only for admin/super_admin accounts. It is also
	// the Agency ID that staff member shares with clients — one value, two names, never two codes.
	AdminID string `bson:"admin_id,omitempty" json:"admin_id,omitempty"`
	PIN     string `bson:"pin,omitempty" json:"-"` // bcrypt-hashed 4-digit PIN, set only for admin/super_admin accounts
	// AgencyID is the Agency ID / Admin ID (e.g. "ADM-7F3K9Q") of the admin this client signed up
	// under — the AdminID of that staff account. Empty means unassigned — visible only to a
	// super_admin, never to a plain admin. Never set on admin/super_admin accounts themselves.
	AgencyID            string     `bson:"agency_id,omitempty" json:"agency_id,omitempty"`
	FailedLoginAttempts int        `bson:"failed_login_attempts" json:"-"`
	LockedUntil         *time.Time `bson:"locked_until,omitempty" json:"-"`
	// LastLoginAt and LoginCount record that this account has actually been used from the app, and
	// when it was last used. They are stamped on every successful sign-in (never on a super admin's
	// impersonation, which would otherwise make a dormant client look active), and are the platform's
	// only evidence of who has the app installed — see the AppPresence constants and the client map.
	LastLoginAt *time.Time `bson:"last_login_at,omitempty" json:"last_login_at,omitempty"`
	LoginCount  int        `bson:"login_count,omitempty" json:"login_count,omitempty"`
	// AdminExpiryDate is set only for role=admin accounts (never for super_admin, which never
	// expires). A Super Admin picks this date when creating the admin; once it passes, the admin
	// can no longer log in until a Super Admin renews it via RenewAdminExpiry.
	AdminExpiryDate *time.Time `bson:"admin_expiry_date,omitempty" json:"admin_expiry_date,omitempty"`
	// LastExpiryAlertSentAt throttles the Super Admin's manual "send expiry alert" action to at
	// most once per cooldown window, so repeated taps don't flood the admin's inbox.
	LastExpiryAlertSentAt *time.Time `bson:"last_expiry_alert_sent_at,omitempty" json:"-"`
	// MergedIntoUserID is set only on the "secondary" side of a Family Merge (see
	// MergeFamilyAccounts): once set, this account is retired for good — its data has been moved to
	// the referenced account and it can never log in again, regardless of IsActive.
	MergedIntoUserID *bson.ObjectID `bson:"merged_into_user_id,omitempty" json:"merged_into_user_id,omitempty"`
	CreatedAt        time.Time      `bson:"created_at" json:"created_at"`
	UpdatedAt        time.Time      `bson:"updated_at" json:"updated_at"`
}

// CreateUserRequest represents the request payload for creating a new user.
type CreateUserRequest struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Phone    string `json:"phone,omitempty"`
	// AgencyID is the Agency ID / Admin ID of the agency the client is signing up under (optional).
	// It is the single code an agency shares: it decides who manages the client and credits that
	// admin with bringing them in. Without it the signup lands unassigned and only a super admin
	// can review it.
	AgencyID string `json:"agency_id,omitempty" example:"ADM-7F3K9Q"`
}

// UpdateUserRequest represents the request payload for updating a user (Admin/Internal).
type UpdateUserRequest struct {
	Name            *string `json:"name,omitempty"`
	Email           *string `json:"email,omitempty"`
	Phone           *string `json:"phone,omitempty"`
	Role            *string `json:"role,omitempty"`
	IsActive        *bool   `json:"is_active,omitempty"`
	IsEmailVerified *bool   `json:"is_email_verified,omitempty"`
	AgencyID        *string `json:"agency_id,omitempty"`
}

// UpdateProfileRequest represents the payload when a logged-in user updates their own profile.
// Note: Email is strictly excluded to enforce email immutability.
type UpdateProfileRequest struct {
	Name  *string `json:"name,omitempty"`
	Phone *string `json:"phone,omitempty"`
}

// ChangePasswordRequest represents the payload for changing a user's password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=6"`
	ConfirmPassword string `json:"confirm_password" binding:"required"`
}

// ChangePINRequest represents the payload for setting or changing a user's 4-digit Security PIN.
// CurrentCredential is checked against whichever secret currently protects the account (PIN if one
// is already set, otherwise Password) — mirroring the interchangeable PIN/Password login model, so
// a client who only has a password can still set their first PIN from here.
type ChangePINRequest struct {
	CurrentCredential string `json:"current_credential" binding:"required" example:"MyP@ssw0rd or 1234"`
	NewPIN            string `json:"new_pin" binding:"required,len=4,numeric" example:"5678"`
}

// UserLoginRequest represents the request payload for unified login for all users (client, advisor, admin, super_admin).
// Accepts UserID / AdminID / Email and PIN / Password interchangeably.
type UserLoginRequest struct {
	Identifier string `json:"identifier,omitempty" example:"ADM-7F3K9Q"`
	UserID     string `json:"user_id,omitempty" example:"user@example.com"`
	Email      string `json:"email,omitempty" example:"user@example.com"`
	AdminID    string `json:"admin_id,omitempty" example:"ADM-7F3K9Q"`
	PIN        string `json:"pin,omitempty" example:"1234"`
	Password   string `json:"password,omitempty" example:"MyP@ssw0rd"`
}

// AdminLoginRequest represents the request payload for admin/super_admin login.
type AdminLoginRequest struct {
	AdminID string `json:"admin_id,omitempty" example:"ADM-7F3K9Q"`
	Email   string `json:"email,omitempty" example:"admin@example.com"`
	PIN     string `json:"pin" binding:"required" example:"1234"`
}

// LoginResponse represents the response containing the token and user details.
type LoginResponse struct {
	Token string        `json:"token"`
	User  *UserResponse `json:"user"`
}

// UserResponse represents the response payload for a user (without sensitive data).
type UserResponse struct {
	ID              bson.ObjectID `json:"id"`
	Name            string        `json:"name"`
	Email           string        `json:"email"`
	Phone           string        `json:"phone,omitempty"`
	Role            string        `json:"role"`
	IsActive        bool          `json:"is_active"`
	IsEmailVerified bool          `json:"is_email_verified"`
	// AdminID doubles as the Agency ID this staff member shares with clients.
	AdminID          string         `json:"admin_id,omitempty"`
	AgencyID         string         `json:"agency_id,omitempty"`
	AdminExpiryDate  *time.Time     `json:"admin_expiry_date,omitempty"`
	MergedIntoUserID *bson.ObjectID `json:"merged_into_user_id,omitempty"`
	// LastLoginAt is nil for an account that has never signed in — which is how staff tell a client
	// who has the app from one who was only ever set up for them.
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	LoginCount  int        `json:"login_count,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// ToResponse converts a User entity to a UserResponse.
func (u *User) ToResponse() *UserResponse {
	return &UserResponse{
		ID:               u.ID,
		Name:             u.Name,
		Email:            u.Email,
		Phone:            u.Phone,
		Role:             u.Role,
		IsActive:         u.IsActive,
		IsEmailVerified:  u.IsEmailVerified,
		AdminID:          u.AdminID,
		AgencyID:         u.AgencyID,
		AdminExpiryDate:  u.AdminExpiryDate,
		MergedIntoUserID: u.MergedIntoUserID,
		LastLoginAt:      u.LastLoginAt,
		LoginCount:       u.LoginCount,
		CreatedAt:        u.CreatedAt,
		UpdatedAt:        u.UpdatedAt,
	}
}

// AdvisorContactDTO is the agency contact a client sees: the admin whose agency they belong to.
// The client app names this person on every "managed by advisor" record and on their profile, so
// "your advisor" stops being an anonymous phrase.
type AdvisorContactDTO struct {
	Name     string `json:"name"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
	AgencyID string `json:"agency_id,omitempty"`

	// AccessExpired reports that this advisor's own access to the platform has lapsed, which the
	// client is told about because it changes what they can expect: the advisor cannot sign in, so
	// nothing on the client's portfolio will be updated by the agency until a super admin renews it.
	//
	// It is deliberately not treated as a problem with the *client's* account. Their data, their
	// documents and everything they maintain themselves keep working exactly as before — this is
	// about the updates that used to arrive from the agency quietly stopping, which is worth saying
	// out loud rather than leaving them to wonder why a premium date never moved.
	AccessExpired bool `json:"access_expired"`
	// AccessExpiresAt is when that access ran out (or will). Nil for a super admin, who never expires.
	AccessExpiresAt *time.Time `json:"access_expires_at,omitempty"`
}

// CreateAdminRequest represents the payload used by a Super Admin to create a new Admin account.
// ExpiryDate is mandatory: every admin account created this way has a fixed validity period after
// which it can no longer log in until a Super Admin renews it (super_admin accounts never expire).
type CreateAdminRequest struct {
	Name  string `json:"name" binding:"required"`
	Email string `json:"email" binding:"required,email"`
	Phone string `json:"phone" binding:"required"`
	// AdminID lets a Super Admin choose the account's Admin ID — which is also the Agency ID this
	// admin shares with clients — instead of taking a generated one. Optional: leave it empty and one
	// is generated. Case-insensitive, and the "ADM-" prefix may be omitted ("asha01" and "ADM-ASHA01"
	// both mean ADM-ASHA01). It must be unique across every account, and is fixed once created,
	// because every client of this agency stores it as their own agency_id.
	AdminID    string    `json:"admin_id,omitempty" example:"ADM-ASHA01"`
	ExpiryDate time.Time `json:"expiry_date" binding:"required" example:"2026-06-30T00:00:00Z"`
}

// SuggestedAdminIDDTO is a freshly generated Admin ID that no account holds yet — what the "generate"
// button on the create-admin form fills itself with, so the Super Admin sees the ID before saving
// rather than finding out afterwards.
type SuggestedAdminIDDTO struct {
	AdminID string `json:"admin_id" example:"ADM-7F3K9Q"`
}

// RenewAdminExpiryRequest represents the payload used by a Super Admin to push an admin account's
// expiry date forward (or otherwise change it). The new date must be in the future.
type RenewAdminExpiryRequest struct {
	ExpiryDate time.Time `json:"expiry_date" binding:"required" example:"2026-12-31T00:00:00Z"`
}

// MergeFamilyAccountsRequest represents the payload a Super Admin submits to fold two separately
// registered client accounts (the same real family, two logins) into one. PrimaryUserID survives
// and keeps its login; every record owned by SecondaryUserID (family members, policies, deposits,
// documents, tickets) is reassigned to PrimaryUserID, after which SecondaryUserID is permanently
// retired — deactivated and marked so it can never sign in again, but never deleted.
type MergeFamilyAccountsRequest struct {
	PrimaryUserID   string `json:"primary_user_id" binding:"required" example:"64f1a2b3c4d5e6f7a8b9c0d1"`
	SecondaryUserID string `json:"secondary_user_id" binding:"required" example:"64f1a2b3c4d5e6f7a8b9c0d2"`
	// ConfirmCrossAgency must be explicitly set true to merge two accounts registered under
	// different Agency IDs — without it, the merge is refused with an error naming both agencies,
	// so a super_admin can't accidentally transplant one agency's client data onto an account
	// another admin manages just by picking the wrong client in the merge picker.
	ConfirmCrossAgency bool `json:"confirm_cross_agency,omitempty"`
}

// MergeFamilyAccountsResult summarizes what moved during a family merge, returned to the Super
// Admin as on-screen confirmation of the outcome.
type MergeFamilyAccountsResult struct {
	Primary              *UserResponse `json:"primary"`
	FamilyMembersMoved   int64         `json:"family_members_moved"`
	LifePoliciesMoved    int64         `json:"life_policies_moved"`
	HealthPoliciesMoved  int64         `json:"health_policies_moved"`
	GeneralPoliciesMoved int64         `json:"general_policies_moved"`
	FixedDepositsMoved   int64         `json:"fixed_deposits_moved"`
	DocumentsMoved       int64         `json:"documents_moved"`
	TicketsMoved         int64         `json:"tickets_moved"`
}

// CreateAdminResponse represents the response returned after successfully creating an Admin account.
// The plaintext TemporaryPassword and TemporaryPIN are shown here ONCE for the Super Admin's convenience
// (e.g. in case the credentials email fails to deliver) — they are never retrievable again afterwards.
type CreateAdminResponse struct {
	Admin             *UserResponse `json:"admin"`
	AdminID           string        `json:"admin_id"`
	Email             string        `json:"email"`
	TemporaryPassword string        `json:"temporary_password"`
	TemporaryPIN      string        `json:"temporary_pin"`
	// The Admin ID above is also this admin's Agency ID — the one code they share with prospective
	// clients, so there is no separate referral code to hand out.
	CredentialsEmailSent bool `json:"credentials_email_sent"`
}

// ImpersonateUserRequest defines the payload for a Super Admin to log in on behalf of a target user or admin.
type ImpersonateUserRequest struct {
	TargetUserID string `json:"target_user_id" binding:"required"`
	Reason       string `json:"reason,omitempty"`
}

// UserRepository defines the interface for user data access operations.
type UserRepository interface {
	Create(ctx context.Context, user *User) (*User, error)
	FindByID(ctx context.Context, id bson.ObjectID) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	// FindByAdminID looks an account up by its Admin ID, which is also its Agency ID — the value a
	// client types at signup to say which agency they belong to.
	FindByAdminID(ctx context.Context, adminID string) (*User, error)
	// FindAll returns a paginated user list. roleFilter and agencyIDFilter narrow the results when
	// non-empty (agencyIDFilter matches the client's AgencyID — i.e. the admin they registered
	// under); pass both empty for the unrestricted platform-wide list (super_admin only).
	FindAll(ctx context.Context, roleFilter, agencyIDFilter, search string, page, limit int64) ([]*User, int64, error)
	FindAllByRoles(ctx context.Context, roles []string, page, limit int64) ([]*User, int64, error)
	// CountActiveClients counts active role=client accounts, narrowed to agencyIDFilter when set.
	CountActiveClients(ctx context.Context, agencyIDFilter string) (int64, error)
	Update(ctx context.Context, id bson.ObjectID, update *UpdateUserRequest) (*User, error)
	UpdatePassword(ctx context.Context, id bson.ObjectID, hashedPassword string) error
	UpdatePIN(ctx context.Context, id bson.ObjectID, hashedPIN string) error
	// MarkMerged deactivates the given account and stamps it as merged into mergedIntoID — used
	// only by MergeFamilyAccounts, on the "secondary" side of a merge.
	MarkMerged(ctx context.Context, id, mergedIntoID bson.ObjectID) error
	MarkEmailVerified(ctx context.Context, id bson.ObjectID) error
	Delete(ctx context.Context, id bson.ObjectID) error
	RecordFailedLogin(ctx context.Context, id bson.ObjectID) (int, error)
	ClearFailedLogins(ctx context.Context, id bson.ObjectID) error
	// RecordLogin stamps a successful sign-in: last_login_at = now, login_count + 1. This is the
	// platform's record of who actually uses the app, so it is called only for a real sign-in —
	// never when a super_admin impersonates someone.
	RecordLogin(ctx context.Context, id bson.ObjectID) error
	LockAccount(ctx context.Context, id bson.ObjectID, until time.Time) error
	// FindExpiringAdmins returns role=admin accounts whose admin_expiry_date is set and falls at or
	// before the given cutoff (so it captures both already-expired admins and those approaching it),
	// sorted soonest-first.
	FindExpiringAdmins(ctx context.Context, cutoff time.Time) ([]*User, error)
	// UpdateAdminExpiry sets a new expiry date on an admin account and clears any previously
	// recorded expiry-alert timestamp so a fresh alert cooldown starts.
	UpdateAdminExpiry(ctx context.Context, id bson.ObjectID, expiryDate time.Time) error
	// RecordExpiryAlertSent stamps the time an expiry-warning email was sent to an admin, used to
	// throttle repeat sends.
	RecordExpiryAlertSent(ctx context.Context, id bson.ObjectID) error
}

// UserService defines the interface for user business logic operations.
type UserService interface {
	Register(ctx context.Context, req *CreateUserRequest) (*UserResponse, error)
	Login(ctx context.Context, req *UserLoginRequest) (*LoginResponse, error)
	AdminLogin(ctx context.Context, req *AdminLoginRequest) (*LoginResponse, error)
	ImpersonateUser(ctx context.Context, superAdminID, targetUserID, reason string) (*LoginResponse, error)
	GetByID(ctx context.Context, requesterRole, requesterID, id string) (*UserResponse, error)
	// GetSelf returns the caller's own profile — every role may always read their own record, so
	// this intentionally bypasses the agency-scoping GetByID applies to admin lookups of others.
	GetSelf(ctx context.Context, id string) (*UserResponse, error)
	// GetMyAdvisor returns the agency contact for this account, or nil when it belongs to no agency
	// (or the agency account has since been removed).
	GetMyAdvisor(ctx context.Context, id string) (*AdvisorContactDTO, error)
	// GetAll returns a paginated user list, scoped by the caller: a super_admin sees everyone; a
	// plain admin sees only clients whose AgencyID matches their own AdminID.
	// search, when non-empty, matches name, email or phone (case-insensitive substring).
	GetAll(ctx context.Context, requesterRole, requesterID, search string, page, limit int64) ([]*UserResponse, int64, error)
	Update(ctx context.Context, requesterRole, requesterID, id string, req *UpdateUserRequest) (*UserResponse, error)
	UpdateProfile(ctx context.Context, id string, req *UpdateProfileRequest) (*UserResponse, error)
	ChangePassword(ctx context.Context, id string, req *ChangePasswordRequest) error
	ChangePIN(ctx context.Context, id string, req *ChangePINRequest) error
	Delete(ctx context.Context, requesterRole, requesterID, id string) error
	DeleteMyAccount(ctx context.Context, userID string) error
	CreateAdmin(ctx context.Context, req *CreateAdminRequest) (*CreateAdminResponse, error)
	// SuggestAdminID returns a generated Admin ID that is free right now. It reserves nothing — the
	// uniqueness that counts is checked when the account is created.
	SuggestAdminID(ctx context.Context) (*SuggestedAdminIDDTO, error)
	GetAllAdmins(ctx context.Context, page, limit int64) ([]*UserResponse, int64, error)
	DeleteAdmin(ctx context.Context, requesterID, targetID string) error
	// MergeFamilyAccounts folds SecondaryUserID's entire data (family members, policies, deposits,
	// documents, tickets) into PrimaryUserID and permanently retires SecondaryUserID's login. Super
	// Admin only, enforced at the router level. Both accounts must be role=client and neither may
	// already be on either side of a previous merge.
	MergeFamilyAccounts(ctx context.Context, requesterID string, req *MergeFamilyAccountsRequest) (*MergeFamilyAccountsResult, error)
	// ListExpiringAdmins returns admin accounts expiring within withinDays (or already expired),
	// soonest-first.
	ListExpiringAdmins(ctx context.Context, withinDays int) ([]*UserResponse, error)
	// RenewAdminExpiry pushes an admin account's expiry date forward, reactivating login if it had
	// already expired, and emails the admin a confirmation.
	RenewAdminExpiry(ctx context.Context, targetID string, req *RenewAdminExpiryRequest) (*UserResponse, error)
	// SendAdminExpiryAlert emails a specific admin a reminder that their access is expiring soon or
	// has expired. Throttled to one alert per cooldown window per admin.
	SendAdminExpiryAlert(ctx context.Context, targetID string) error
}
