package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// EmailVerification represents an OTP record for signup email ownership verification.
type EmailVerification struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Email     string        `bson:"email" json:"email"`
	OTP       string        `bson:"otp" json:"otp"`
	ExpiresAt time.Time     `bson:"expires_at" json:"expires_at"`
	IsUsed    bool          `bson:"is_used" json:"is_used"`
	Attempts  int           `bson:"attempts" json:"attempts"`
	CreatedAt time.Time     `bson:"created_at" json:"created_at"`
}

// VerifyEmailOTPRequest represents the payload for verifying email OTP code.
type VerifyEmailOTPRequest struct {
	Email string `json:"email" binding:"required,email" example:"user@example.com"`
	OTP   string `json:"otp" binding:"required,len=6" example:"123456"`
}

// ResendEmailOTPRequest represents the payload for requesting a fresh email verification OTP.
type ResendEmailOTPRequest struct {
	Email string `json:"email" binding:"required,email" example:"user@example.com"`
}

// EmailVerificationRepository defines data access methods for email verification records.
type EmailVerificationRepository interface {
	Create(ctx context.Context, verif *EmailVerification) (*EmailVerification, error)
	FindLatestActiveOTP(ctx context.Context, email string) (*EmailVerification, error)
	IncrementAttempts(ctx context.Context, id bson.ObjectID) error
	MarkAsUsed(ctx context.Context, id bson.ObjectID) error
	DeleteAllByEmail(ctx context.Context, email string) error
}

// EmailVerificationService defines business logic methods for email OTP verification.
type EmailVerificationService interface {
	VerifyOTP(ctx context.Context, req *VerifyEmailOTPRequest) error
	ResendOTP(ctx context.Context, req *ResendEmailOTPRequest) error
}
