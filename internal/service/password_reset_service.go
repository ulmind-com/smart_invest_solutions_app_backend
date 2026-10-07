package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/email"
	"github.com/smart-invest-solutions/backend/pkg/utils"
	"golang.org/x/crypto/bcrypt"
)

type passwordResetService struct {
	resetRepo domain.PasswordResetRepository
	userRepo  domain.UserRepository
	emailSvc  email.EmailService
}

// NewPasswordResetService creates a new instance of PasswordResetService.
func NewPasswordResetService(
	resetRepo domain.PasswordResetRepository,
	userRepo domain.UserRepository,
	emailSvc email.EmailService,
) domain.PasswordResetService {
	return &passwordResetService{
		resetRepo: resetRepo,
		userRepo:  userRepo,
		emailSvc:  emailSvc,
	}
}

// generate6DigitOTP generates a cryptographically secure 6-digit numeric OTP string.
func generate6DigitOTP() (string, error) {
	nBig, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", nBig.Int64()+100000), nil
}

// SendOTP generates and emails a password reset OTP code.
//
// It enforces a 60-second resend cooldown (mirroring the signup email-verification flow) so this
// public, unauthenticated endpoint can't be spammed to flood a victim's inbox or run up
// email-provider costs, and the route itself is rate limited per IP.
//
// An address with no account is reported as such rather than answered with a generic success: see
// domain.UnknownAccountError for why that trade-off is made deliberately. An account that exists but
// could never sign in after a reset is reported too, because sending it a code would be a dead end.
func (s *passwordResetService) SendOTP(ctx context.Context, req *domain.ForgotPasswordRequest) error {
	req.Email = utils.NormalizeEmail(req.Email)
	if req.Email == "" {
		return &domain.UnknownAccountError{Message: "Enter the email address your account is registered with"}
	}

	user, err := s.userRepo.FindByEmail(ctx, req.Email)
	if err != nil || user == nil {
		return &domain.UnknownAccountError{
			Message: fmt.Sprintf("No account is registered with %s. Check the spelling, or request access to create one.", req.Email),
		}
	}

	// A retired account can never sign in again whatever its password is, so a reset is a dead end.
	if user.MergedIntoUserID != nil {
		return &domain.ResetUnavailableError{
			Message: "This account was merged into another family account. Reset the password on that account instead.",
		}
	}

	// Login refuses an unverified client whatever the password is, so a reset wouldn't get them in.
	// Finishing the signup OTP is the step that actually unblocks them.
	if user.Role == domain.RoleClient && !user.IsEmailVerified {
		return &domain.ResetUnavailableError{
			Message: "This email hasn't been verified yet. Finish signing up with the code sent to your inbox — you'll choose your password there.",
		}
	}

	// Enforce 60-second rate limit cooldown before touching any existing OTP record.
	if latest, _ := s.resetRepo.FindLatestActiveOTP(ctx, req.Email); latest != nil {
		if remaining := 60*time.Second - time.Since(latest.CreatedAt); remaining > 0 {
			return &domain.CooldownError{Message: fmt.Sprintf("please wait %s before requesting a new OTP code", remaining.Round(time.Second))}
		}
	}

	// Clean up any old OTP records for this email first
	_ = s.resetRepo.DeleteAllByEmail(ctx, req.Email)

	// Generate 6-digit OTP
	otpCode, err := generate6DigitOTP()
	if err != nil {
		return fmt.Errorf("failed to generate OTP: %w", err)
	}

	// Create OTP record valid for 10 minutes
	reset := &domain.PasswordReset{
		Email:     req.Email,
		OTP:       otpCode,
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
		IsUsed:    false,
	}

	_, err = s.resetRepo.Create(ctx, reset)
	if err != nil {
		return fmt.Errorf("failed to store OTP: %w", err)
	}

	// Sent in the background: delivery takes seconds and the caller only needs to know the code was
	// issued. Guarded on the service being wired, because a nil dereference in a goroutine is not a
	// failed email — it panics the whole process, taking every other request down with it.
	if s.emailSvc != nil {
		email, code := req.Email, otpCode
		go func() {
			if err := s.emailSvc.SendOTPEmail(context.Background(), email, code); err != nil {
				log.Error().Err(err).Str("email", email).Msg("failed to send password reset OTP email")
			}
		}()
	} else {
		log.Error().Str("email", req.Email).Msg("no email service configured — password reset OTP was stored but not sent")
	}

	return nil
}

// checkOTP looks up the active OTP record for an email and validates the submitted code against
// it, capping wrong guesses at 5 attempts — the same lockout the signup email-verification flow
// uses, so a 6-digit reset code can't be brute-forced within its 10-minute validity window.
func (s *passwordResetService) checkOTP(ctx context.Context, email, otp string) error {
	reset, err := s.resetRepo.FindLatestActiveOTP(ctx, email)
	if err != nil {
		return fmt.Errorf("failed to verify OTP: %w", err)
	}
	if reset == nil {
		return fmt.Errorf("invalid or expired OTP code")
	}
	if reset.Attempts >= 5 {
		return fmt.Errorf("too many invalid OTP attempts. Please request a new code")
	}
	if reset.OTP != otp {
		_ = s.resetRepo.IncrementAttempts(ctx, reset.ID)
		return fmt.Errorf("invalid OTP code. Please check your email and try again")
	}
	return nil
}

// VerifyOTP verifies if the provided OTP code is valid, unexpired, and unused.
func (s *passwordResetService) VerifyOTP(ctx context.Context, req *domain.VerifyOTPRequest) error {
	req.Email = utils.NormalizeEmail(req.Email)
	return s.checkOTP(ctx, req.Email, req.OTP)
}

// ResetPassword verifies the OTP and updates the user's password in MongoDB.
func (s *passwordResetService) ResetPassword(ctx context.Context, req *domain.ResetPasswordRequest) error {
	req.Email = utils.NormalizeEmail(req.Email)
	if req.NewPassword != req.ConfirmPassword {
		return fmt.Errorf("new password and confirm password do not match")
	}

	// Verify active OTP
	if err := s.checkOTP(ctx, req.Email, req.OTP); err != nil {
		return err
	}

	// Find user
	user, err := s.userRepo.FindByEmail(ctx, req.Email)
	if err != nil || user == nil {
		return fmt.Errorf("user account not found")
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Update user password in MongoDB
	err = s.userRepo.UpdatePassword(ctx, user.ID, string(hashedPassword))
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	// Proving ownership of the inbox is exactly what a lockout waits for — a user who just reset
	// their password must be able to sign in with it straight away.
	_ = s.userRepo.ClearFailedLogins(ctx, user.ID)

	// Immediately delete all OTP records for this email to prevent DB storage bloat
	_ = s.resetRepo.DeleteAllByEmail(ctx, req.Email)

	// Send confirmation email asynchronously
	go func() {
		if err := s.emailSvc.SendPasswordResetConfirmationEmail(context.Background(), req.Email); err != nil {
			log.Error().Err(err).Str("email", req.Email).Msg("failed to send password reset confirmation email")
		}
	}()

	return nil
}
