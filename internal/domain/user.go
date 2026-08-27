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
	CreatedAt           time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt           time.Time     `bson:"updated_at" json:"updated_at"`
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

// UserLoginRequest represents the request payload for normal user (client/advisor) login.
// Only Email + Password is accepted — admin_id/pin credentials are rejected here.
type UserLoginRequest struct {
	Email    string `json:"email" binding:"required,email" example:"user@example.com"`
	Password string `json:"password" binding:"required" example:"MyP@ssw0rd"`
}

// AdminLoginRequest represents the request payload for admin/super_admin login.
// Only AdminID + PIN is accepted — email/password credentials are rejected here.
type AdminLoginRequest struct {
	AdminID string `json:"admin_id" binding:"required" example:"ADM-7F3K9Q"`
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
		CreatedAt:          u.CreatedAt,
		UpdatedAt:          u.UpdatedAt,
	}
}

// CreateAdminRequest represents the payload used by a Super Admin to create a new Admin account.
type CreateAdminRequest struct {
	Name  string `json:"name" binding:"required"`
	Email string `json:"email" binding:"required,email"`
	Phone string `json:"phone" binding:"required"`
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
}
