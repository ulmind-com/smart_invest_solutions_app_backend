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
	// Attempts counts wrong-OTP guesses against this record — capped at 5 (see VerifyOTP/
	// ResetPassword), the same lockout used for signup email verification, so a 6-digit reset code
	// can't be brute-forced within its 10-minute validity window.
	Attempts  int       `bson:"attempts" json:"attempts"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
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
	NewPassword     string `json:"new_password" binding:"required,min=6"`
	ConfirmPassword string `json:"confirm_password" binding:"required"`
}

// PasswordResetRepository defines data access methods for OTP records.
type PasswordResetRepository interface {
	Create(ctx context.Context, reset *PasswordReset) (*PasswordReset, error)
	// FindLatestActiveOTP returns the most recent unexpired, unused OTP record for an email —
	// matched by email alone (not by OTP value), so the caller can compare the submitted code
	// itself and track wrong-guess attempts against one identified record, the same pattern
	// EmailVerificationRepository uses.
	FindLatestActiveOTP(ctx context.Context, email string) (*PasswordReset, error)
	IncrementAttempts(ctx context.Context, id bson.ObjectID) error
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

// UnknownAccountError reports that no account is registered with the address a password reset was
// requested for.
//
// This is deliberately told to the caller. The alternative — answering every address with "if an
// account exists, a code has been sent" — hides which addresses are registered, but it also leaves
// someone who simply mistyped their email waiting for a code that will never arrive, with nothing on
// screen to explain why. The product choice here is to say so plainly; the endpoint is rate limited
// per IP so the answer can't be used to enumerate the user base in bulk.
type UnknownAccountError struct {
	Message string
}

func (e *UnknownAccountError) Error() string { return e.Message }

// ResetUnavailableError reports that the account exists but a password reset cannot help it sign in
// — so sending a code would be a dead end. Two cases reach here: an account retired by a family
// merge, and a signup whose email was never verified (login refuses it whatever the password is).
type ResetUnavailableError struct {
	Message string
}

func (e *ResetUnavailableError) Error() string { return e.Message }

// CooldownError reports that an action (e.g. sending another OTP) was refused only because the
// previous one was too recent. Handlers surface it as 429 so the app can tell the user to wait
// instead of claiming a code was sent.
type CooldownError struct {
	Message string
}

func (e *CooldownError) Error() string { return e.Message }
