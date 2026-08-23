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
	AdminID             string        `bson:"admin_id,omitempty" json:"admin_id,omitempty"` // Unique login ID, set only for admin/super_admin accounts
	PIN                 string        `bson:"pin,omitempty" json:"-"`                        // bcrypt-hashed 4-digit PIN, set only for admin/super_admin accounts
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
	Name     *string `json:"name,omitempty"`
	Email    *string `json:"email,omitempty"`
	Phone    *string `json:"phone,omitempty"`
	Role     *string `json:"role,omitempty"`
	IsActive *bool   `json:"is_active,omitempty"`
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

// LoginRequest represents the request payload for user login.
// Clients authenticate with Email + Password. Admin/Super Admin accounts may additionally
// authenticate with AdminID in place of Email, and/or PIN in place of Password — any
// combination of identifier (Email or AdminID) + credential (Password or PIN) is accepted.
type LoginRequest struct {
	Email    string `json:"email,omitempty" example:"user@example.com"`
	AdminID  string `json:"admin_id,omitempty" example:"ADM-7F3K9Q"`
	Password string `json:"password,omitempty" example:"MyP@ssw0rd"`
	PIN      string `json:"pin,omitempty" example:"1234"`
}

// LoginResponse represents the response containing the token and user details.
type LoginResponse struct {
	Token string        `json:"token"`
	User  *UserResponse `json:"user"`
}

// UserResponse represents the response payload for a user (without sensitive data).
type UserResponse struct {
	ID        bson.ObjectID `json:"id"`
	Name      string        `json:"name"`
	Email     string        `json:"email"`
	Phone     string        `json:"phone,omitempty"`
	Role      string        `json:"role"`
	IsActive  bool          `json:"is_active"`
	AdminID   string        `json:"admin_id,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// ToResponse converts a User entity to a UserResponse.
func (u *User) ToResponse() *UserResponse {
	return &UserResponse{
		ID:        u.ID,
		Name:      u.Name,
		Email:     u.Email,
		Phone:     u.Phone,
		Role:      u.Role,
		IsActive:  u.IsActive,
		AdminID:   u.AdminID,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
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

// UserRepository defines the interface for user data access operations.
type UserRepository interface {
	Create(ctx context.Context, user *User) (*User, error)
	FindByID(ctx context.Context, id bson.ObjectID) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	FindByEmailOrAdminID(ctx context.Context, identifier string) (*User, error)
	FindAll(ctx context.Context, page, limit int64) ([]*User, int64, error)
	FindAllByRoles(ctx context.Context, roles []string, page, limit int64) ([]*User, int64, error)
	Update(ctx context.Context, id bson.ObjectID, update *UpdateUserRequest) (*User, error)
	UpdatePassword(ctx context.Context, id bson.ObjectID, hashedPassword string) error
	Delete(ctx context.Context, id bson.ObjectID) error
	RecordFailedLogin(ctx context.Context, id bson.ObjectID) (int, error)
	ClearFailedLogins(ctx context.Context, id bson.ObjectID) error
	LockAccount(ctx context.Context, id bson.ObjectID, until time.Time) error
}

// UserService defines the interface for user business logic operations.
type UserService interface {
	Register(ctx context.Context, req *CreateUserRequest) (*UserResponse, error)
	Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error)
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
