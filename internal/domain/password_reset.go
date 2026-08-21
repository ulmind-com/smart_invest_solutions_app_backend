package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// PasswordReset represents an OTP record stored in MongoDB for password resets.
type PasswordReset struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Email     string        `bson:"email" json:"email"`
	OTP       string        `bson:"otp" json:"otp"`
	ExpiresAt time.Time     `bson:"expires_at" json:"expires_at"`
	IsUsed    bool          `bson:"is_used" json:"is_used"`
	CreatedAt time.Time     `bson:"created_at" json:"created_at"`
}

// ForgotPasswordRequest represents the payload for requesting a password reset OTP.
type ForgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// VerifyOTPRequest represents the payload for verifying the OTP code.
type VerifyOTPRequest struct {
	Email string `json:"email" binding:"required,email"`
	OTP   string `json:"otp" binding:"required,len=6"`
}

// ResetPasswordRequest represents the payload for resetting the password after OTP verification.
type ResetPasswordRequest struct {
	Email           string `json:"email" binding:"required,email"`
	OTP             string `json:"otp" binding:"required,len=6"`
	NewPassword     string `json:"new_password" binding:"required"`
	ConfirmPassword string `json:"confirm_password" binding:"required"`
}

// PasswordResetRepository defines data access methods for OTP records.
type PasswordResetRepository interface {
	Create(ctx context.Context, reset *PasswordReset) (*PasswordReset, error)
	FindLatestActiveOTP(ctx context.Context, email, otp string) (*PasswordReset, error)
	MarkAsUsed(ctx context.Context, id bson.ObjectID) error
	DeleteAllByEmail(ctx context.Context, email string) error
	DeleteByID(ctx context.Context, id bson.ObjectID) error
}

// PasswordResetService defines business logic methods for password reset.
type PasswordResetService interface {
	SendOTP(ctx context.Context, req *ForgotPasswordRequest) error
	VerifyOTP(ctx context.Context, req *VerifyOTPRequest) error
	ResetPassword(ctx context.Context, req *ResetPasswordRequest) error
}
