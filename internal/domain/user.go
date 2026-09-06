package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Role constants
const (
	RoleClient     = "client"
	RoleAdvisor    = "advisor"
	RoleAdmin      = "admin"
	RoleSuperAdmin = "super_admin"
)

// User represents a user entity in the system.
type User struct {
	ID                  bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Name                string        `bson:"name" json:"name" binding:"required"`
	Email               string        `bson:"email" json:"email" binding:"required,email"`
	Password            string        `bson:"password" json:"-"`
	Phone               string        `bson:"phone,omitempty" json:"phone,omitempty"`
	Role                string        `bson:"role" json:"role"` // client, advisor, admin, super_admin
	IsActive            bool          `bson:"is_active" json:"is_active"`
	IsEmailVerified     bool          `bson:"is_email_verified" json:"is_email_verified"`
	AdminID             string        `bson:"admin_id,omitempty" json:"admin_id,omitempty"` // Unique login ID, set only for admin/super_admin accounts
	PIN                 string        `bson:"pin,omitempty" json:"-"`                        // bcrypt-hashed 4-digit PIN, set only for admin/super_admin accounts
	ReferralCode        string        `bson:"referral_code,omitempty" json:"referral_code,omitempty"`
	AppValidityEndDate  time.Time     `bson:"app_validity_end_date,omitempty" json:"app_validity_end_date,omitempty"`
	FailedLoginAttempts int           `bson:"failed_login_attempts" json:"-"`
	LockedUntil         *time.Time    `bson:"locked_until,omitempty" json:"-"`
	// AdminExpiryDate is set only for role=admin accounts (never for super_admin, which never
	// expires). A Super Admin picks this date when creating the admin; once it passes, the admin
	// can no longer log in until a Super Admin renews it via RenewAdminExpiry.
	AdminExpiryDate *time.Time `bson:"admin_expiry_date,omitempty" json:"admin_expiry_date,omitempty"`
	// LastExpiryAlertSentAt throttles the Super Admin's manual "send expiry alert" action to at
	// most once per cooldown window, so repeated taps don't flood the admin's inbox.
	LastExpiryAlertSentAt *time.Time `bson:"last_expiry_alert_sent_at,omitempty" json:"-"`
	CreatedAt             time.Time  `bson:"created_at" json:"created_at"`
	UpdatedAt             time.Time  `bson:"updated_at" json:"updated_at"`
}

// CreateUserRequest represents the request payload for creating a new user.
type CreateUserRequest struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
	Phone    string `json:"phone,omitempty"`
}

// UpdateUserRequest represents the request payload for updating a user (Admin/Internal).
type UpdateUserRequest struct {
	Name            *string `json:"name,omitempty"`
	Email           *string `json:"email,omitempty"`
	Phone           *string `json:"phone,omitempty"`
	Role            *string `json:"role,omitempty"`
	IsActive        *bool   `json:"is_active,omitempty"`
	IsEmailVerified *bool   `json:"is_email_verified,omitempty"`
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
	NewPassword     string `json:"new_password" binding:"required"`
	ConfirmPassword string `json:"confirm_password" binding:"required"`
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
	ID                 bson.ObjectID `json:"id"`
	Name               string        `json:"name"`
	Email              string        `json:"email"`
	Phone              string        `json:"phone,omitempty"`
	Role               string        `json:"role"`
	IsActive           bool          `json:"is_active"`
	IsEmailVerified    bool          `json:"is_email_verified"`
	AdminID            string        `json:"admin_id,omitempty"`
	ReferralCode       string        `json:"referral_code,omitempty"`
	AppValidityEndDate time.Time     `json:"app_validity_end_date,omitempty"`
	AdminExpiryDate    *time.Time    `json:"admin_expiry_date,omitempty"`
	CreatedAt          time.Time     `json:"created_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
}

// ToResponse converts a User entity to a UserResponse.
func (u *User) ToResponse() *UserResponse {
	return &UserResponse{
		ID:                 u.ID,
		Name:               u.Name,
		Email:              u.Email,
		Phone:              u.Phone,
		Role:               u.Role,
		IsActive:           u.IsActive,
		IsEmailVerified:    u.IsEmailVerified,
		AdminID:            u.AdminID,
		ReferralCode:       u.ReferralCode,
		AppValidityEndDate: u.AppValidityEndDate,
		AdminExpiryDate:    u.AdminExpiryDate,
		CreatedAt:          u.CreatedAt,
		UpdatedAt:          u.UpdatedAt,
	}
}

// CreateAdminRequest represents the payload used by a Super Admin to create a new Admin account.
// ExpiryDate is mandatory: every admin account created this way has a fixed validity period after
// which it can no longer log in until a Super Admin renews it (super_admin accounts never expire).
type CreateAdminRequest struct {
	Name       string    `json:"name" binding:"required"`
	Email      string    `json:"email" binding:"required,email"`
	Phone      string    `json:"phone" binding:"required"`
	ExpiryDate time.Time `json:"expiry_date" binding:"required" example:"2026-06-30T00:00:00Z"`
}

// RenewAdminExpiryRequest represents the payload used by a Super Admin to push an admin account's
// expiry date forward (or otherwise change it). The new date must be in the future.
type RenewAdminExpiryRequest struct {
	ExpiryDate time.Time `json:"expiry_date" binding:"required" example:"2026-12-31T00:00:00Z"`
}

// CreateAdminResponse represents the response returned after successfully creating an Admin account.
// The plaintext TemporaryPassword and TemporaryPIN are shown here ONCE for the Super Admin's convenience
// (e.g. in case the credentials email fails to deliver) — they are never retrievable again afterwards.
type CreateAdminResponse struct {
	Admin                *UserResponse `json:"admin"`
	AdminID              string        `json:"admin_id"`
	Email                string        `json:"email"`
	TemporaryPassword    string        `json:"temporary_password"`
	TemporaryPIN         string        `json:"temporary_pin"`
	CredentialsEmailSent bool          `json:"credentials_email_sent"`
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
	FindByAdminID(ctx context.Context, adminID string) (*User, error)
	FindByReferralCode(ctx context.Context, code string) (*User, error)
	FindAll(ctx context.Context, page, limit int64) ([]*User, int64, error)
	FindAllByRoles(ctx context.Context, roles []string, page, limit int64) ([]*User, int64, error)
	Update(ctx context.Context, id bson.ObjectID, update *UpdateUserRequest) (*User, error)
	UpdatePassword(ctx context.Context, id bson.ObjectID, hashedPassword string) error
	MarkEmailVerified(ctx context.Context, id bson.ObjectID) error
	ExtendValidity(ctx context.Context, userID bson.ObjectID, extraDays int) error
	Delete(ctx context.Context, id bson.ObjectID) error
	RecordFailedLogin(ctx context.Context, id bson.ObjectID) (int, error)
	ClearFailedLogins(ctx context.Context, id bson.ObjectID) error
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
	GetByID(ctx context.Context, id string) (*UserResponse, error)
	GetAll(ctx context.Context, page, limit int64) ([]*UserResponse, int64, error)
	Update(ctx context.Context, requesterRole, id string, req *UpdateUserRequest) (*UserResponse, error)
	UpdateProfile(ctx context.Context, id string, req *UpdateProfileRequest) (*UserResponse, error)
	ChangePassword(ctx context.Context, id string, req *ChangePasswordRequest) error
	Delete(ctx context.Context, requesterRole, id string) error
	DeleteMyAccount(ctx context.Context, userID string) error
	CreateAdmin(ctx context.Context, req *CreateAdminRequest) (*CreateAdminResponse, error)
	GetAllAdmins(ctx context.Context, page, limit int64) ([]*UserResponse, int64, error)
	DeleteAdmin(ctx context.Context, requesterID, targetID string) error
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
