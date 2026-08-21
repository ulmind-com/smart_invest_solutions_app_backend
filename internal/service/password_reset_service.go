package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/email"
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

// SendOTP handles generating and emailing a password reset OTP code.
func (s *passwordResetService) SendOTP(ctx context.Context, req *domain.ForgotPasswordRequest) error {
	// Verify user exists (Do not leak specific errors to prevent user enumeration attacks)
	user, err := s.userRepo.FindByEmail(ctx, req.Email)
	if err != nil || user == nil {
		// User does not exist, but we return nil so caller receives generic success message
		return nil
	}

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

	// Send OTP email asynchronously / safely
	go func() {
		_ = s.emailSvc.SendOTPEmail(context.Background(), req.Email, otpCode)
	}()

	return nil
}

// VerifyOTP verifies if the provided OTP code is valid, unexpired, and unused.
func (s *passwordResetService) VerifyOTP(ctx context.Context, req *domain.VerifyOTPRequest) error {
	_, err := s.resetRepo.FindLatestActiveOTP(ctx, req.Email, req.OTP)
	if err != nil {
		return fmt.Errorf("invalid or expired OTP code")
	}
	return nil
}

// ResetPassword verifies the OTP and updates the user's password in MongoDB.
func (s *passwordResetService) ResetPassword(ctx context.Context, req *domain.ResetPasswordRequest) error {
	if req.NewPassword != req.ConfirmPassword {
		return fmt.Errorf("new password and confirm password do not match")
	}

	if len(req.NewPassword) < 8 {
		return fmt.Errorf("password must be at least 8 characters long")
	}

	// Verify active OTP
	otpRecord, err := s.resetRepo.FindLatestActiveOTP(ctx, req.Email, req.OTP)
	if err != nil {
		return fmt.Errorf("invalid or expired OTP code")
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

	// Mark OTP as used
	_ = s.resetRepo.MarkAsUsed(ctx, otpRecord.ID)

	// Send confirmation email asynchronously
	go func() {
		_ = s.emailSvc.SendPasswordResetConfirmationEmail(context.Background(), req.Email)
	}()

	return nil
}
