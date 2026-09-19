package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/email"
	"github.com/smart-invest-solutions/backend/pkg/utils"
)

type emailVerificationService struct {
	verifRepo     domain.EmailVerificationRepository
	userRepo      domain.UserRepository
	accessReqRepo domain.AccessRequestRepository
	emailSvc      email.EmailService
}

// NewEmailVerificationService initializes a new EmailVerificationService instance.
func NewEmailVerificationService(
	verifRepo domain.EmailVerificationRepository,
	userRepo domain.UserRepository,
	accessReqRepo domain.AccessRequestRepository,
	emailSvc email.EmailService,
) domain.EmailVerificationService {
	return &emailVerificationService{
		verifRepo:     verifRepo,
		userRepo:      userRepo,
		accessReqRepo: accessReqRepo,
		emailSvc:      emailSvc,
	}
}

// VerifyOTP validates the 6-digit OTP entered by the user, marks the email as verified,
// creates an AccessRequest for Admin review, and sends a Welcome confirmation email.
func (s *emailVerificationService) VerifyOTP(ctx context.Context, req *domain.VerifyEmailOTPRequest) error {
	req.Email = utils.NormalizeEmail(req.Email)
	req.OTP = strings.TrimSpace(req.OTP)
	user, err := s.userRepo.FindByEmail(ctx, req.Email)
	if err != nil || user == nil {
		return fmt.Errorf("user account with email %s not found", req.Email)
	}

	if user.IsEmailVerified {
		return fmt.Errorf("email address %s is already verified", req.Email)
	}

	verif, err := s.verifRepo.FindLatestActiveOTP(ctx, req.Email)
	if err != nil {
		return fmt.Errorf("failed to verify OTP: %w", err)
	}
	if verif == nil {
		return fmt.Errorf("invalid or expired OTP code. Please request a new code")
	}

	if verif.Attempts >= 5 {
		return fmt.Errorf("too many invalid OTP attempts. Please request a new code")
	}

	if verif.OTP != req.OTP {
		_ = s.verifRepo.IncrementAttempts(ctx, verif.ID)
		return fmt.Errorf("invalid OTP code. Please check your email and try again")
	}

	// OTP matched! Mark record as used & update user email status
	if err := s.verifRepo.MarkAsUsed(ctx, verif.ID); err != nil {
		return fmt.Errorf("failed to complete OTP verification: %w", err)
	}

	if err := s.userRepo.MarkEmailVerified(ctx, user.ID); err != nil {
		return fmt.Errorf("failed to update email verification status: %w", err)
	}

	// Trigger Admin verification queueing now that email ownership is verified
	if s.accessReqRepo != nil {
		// Queue the verified signup for review under the agency it signed up with, so that agency's
		// admin (not only a super admin) sees it.
		existingReq, _ := s.accessReqRepo.FindByEmail(ctx, user.Email)
		switch {
		case existingReq == nil:
			accessReq := &domain.AccessRequest{
				Name:            user.Name,
				Email:           user.Email,
				Phone:           user.Phone,
				Notes:           "Signup email ownership verified via OTP",
				AppliedAgencyID: user.AgencyID,
				Status:          domain.AccessStatusPending,
			}
			if _, err := s.accessReqRepo.Create(ctx, accessReq); err != nil {
				log.Error().Err(err).Str("email", user.Email).Msg("failed to queue verified signup for review")
			}
		case existingReq.Status == domain.AccessStatusRejected:
			// A previously rejected applicant signing up again goes back into the review queue.
			if _, err := s.accessReqRepo.UpdateDetailsAndStatus(ctx, existingReq.ID, user.Name, user.Phone,
				"Signup email ownership verified via OTP", existingReq.AppliedReferralCode, user.AgencyID,
				domain.AccessStatusPending); err != nil {
				log.Error().Err(err).Str("email", user.Email).Msg("failed to re-queue verified signup for review")
			}
		}
	}

	if s.emailSvc != nil {
		go func() {
			if err := s.emailSvc.SendWelcomeEmail(context.Background(), user.Email, user.Name); err != nil {
				log.Error().Err(err).Str("email", user.Email).Msg("failed to send welcome email")
			}
		}()
	}

	return nil
}

// ResendOTP generates and sends a fresh 6-digit OTP code, enforcing a 60-second cooldown limit.
func (s *emailVerificationService) ResendOTP(ctx context.Context, req *domain.ResendEmailOTPRequest) error {
	req.Email = utils.NormalizeEmail(req.Email)
	user, err := s.userRepo.FindByEmail(ctx, req.Email)
	if err != nil || user == nil {
		return fmt.Errorf("user account with email %s not found", req.Email)
	}

	if user.IsEmailVerified {
		return fmt.Errorf("email address %s is already verified", req.Email)
	}

	// Enforce 60-second rate limit cooldown
	latestVerif, _ := s.verifRepo.FindLatestActiveOTP(ctx, req.Email)
	if latestVerif != nil && time.Since(latestVerif.CreatedAt) < 60*time.Second {
		remaining := (60*time.Second - time.Since(latestVerif.CreatedAt)).Round(time.Second)
		return fmt.Errorf("please wait %s before requesting a new OTP code", remaining)
	}

	// Generate 6-digit numeric OTP
	otpCode, err := utils.GenerateNumericCode(6)
	if err != nil {
		return fmt.Errorf("failed to generate OTP: %w", err)
	}

	verifRecord := &domain.EmailVerification{
		Email:     user.Email,
		OTP:       otpCode,
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
		IsUsed:    false,
		Attempts:  0,
	}

	if _, err := s.verifRepo.Create(ctx, verifRecord); err != nil {
		return fmt.Errorf("failed to save OTP record: %w", err)
	}

	// Send OTP email
	if s.emailSvc != nil {
		go func() {
			if err := s.emailSvc.SendVerificationOTPEmail(context.Background(), user.Email, user.Name, otpCode); err != nil {
				log.Error().Err(err).Str("email", user.Email).Msg("failed to send verification OTP email")
			}
		}()
	}

	return nil
}
