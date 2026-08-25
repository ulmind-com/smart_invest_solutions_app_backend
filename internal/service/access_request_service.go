package service

import (
	"context"
	"fmt"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/email"
	"github.com/smart-invest-solutions/backend/pkg/utils"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type accessRequestService struct {
	repo         domain.AccessRequestRepository
	userRepo     domain.UserRepository
	userService  domain.UserService
	emailSvc     email.EmailService
	referralRepo domain.ReferralRepository
}

// NewAccessRequestService creates a new AccessRequest service instance.
func NewAccessRequestService(
	repo domain.AccessRequestRepository,
	userRepo domain.UserRepository,
	userService domain.UserService,
	emailSvc email.EmailService,
	referralRepo domain.ReferralRepository,
) domain.AccessRequestService {
	return &accessRequestService{
		repo:         repo,
		userRepo:     userRepo,
		userService:  userService,
		emailSvc:     emailSvc,
		referralRepo: referralRepo,
	}
}

// SubmitRequest handles client access request submission.
func (s *accessRequestService) SubmitRequest(ctx context.Context, dto *domain.CreateAccessRequestDTO) (*domain.AccessRequest, error) {
	// Check if user already exists
	existingUser, _ := s.userRepo.FindByEmail(ctx, dto.Email)
	if existingUser != nil {
		return nil, fmt.Errorf("an account with email %s already exists. Please login directly", dto.Email)
	}

	// Check if a pending request already exists for this email
	existingReq, _ := s.repo.FindByEmail(ctx, dto.Email)
	if existingReq != nil && existingReq.Status == domain.AccessStatusPending {
		return nil, fmt.Errorf("an access request for email %s is already pending approval", dto.Email)
	}

	accessReq := &domain.AccessRequest{
		Name:                dto.Name,
		Email:               dto.Email,
		Phone:               dto.Phone,
		Notes:               dto.Notes,
		AppliedReferralCode: dto.AppliedReferralCode,
		Status:              domain.AccessStatusPending,
	}

	createdReq, err := s.repo.Create(ctx, accessReq)
	if err != nil {
		return nil, err
	}

	// Referral tracking hook: Create a Pending ReferralRecord if a valid referral code was applied
	if dto.AppliedReferralCode != "" && s.referralRepo != nil {
		referrer, _ := s.userRepo.FindByReferralCode(ctx, dto.AppliedReferralCode)
		if referrer != nil && referrer.Email != dto.Email {
			pendingRecord := &domain.ReferralRecord{
				ReferrerID:         referrer.ID,
				ReferredEmail:      dto.Email,
				Status:             domain.ReferralStatusPending,
				RewardDaysCredited: 0,
			}
			_, _ = s.referralRepo.Create(ctx, pendingRecord)
		}
	}

	return createdReq, nil
}

// GetAllRequests retrieves all access requests with optional status filter & pagination.
func (s *accessRequestService) GetAllRequests(ctx context.Context, status string, page, limit int64) ([]*domain.AccessRequest, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	return s.repo.FindAll(ctx, status, page, limit)
}

// GetRequestByID retrieves a single request by ID.
func (s *accessRequestService) GetRequestByID(ctx context.Context, id string) (*domain.AccessRequest, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid access request ID format")
	}
	return s.repo.FindByID(ctx, objectID)
}

// ApproveRequest approves a client access request, activates user account (IsActive = true),
// sends credentials/approval email, and executes referral reward hook.
func (s *accessRequestService) ApproveRequest(ctx context.Context, id string, dto *domain.ApproveAccessRequestDTO) (*domain.UserResponse, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid access request ID format")
	}

	accessReq, err := s.repo.FindByID(ctx, objectID)
	if err != nil {
		return nil, err
	}

	if accessReq.Status == domain.AccessStatusApproved {
		return nil, fmt.Errorf("this access request has already been approved")
	}

	var userResp *domain.UserResponse
	var passwordSent string

	// Check if user already registered directly
	existingUser, _ := s.userRepo.FindByEmail(ctx, accessReq.Email)
	trueVal := true

	if existingUser != nil {
		// Activate existing user account
		updatedUser, err := s.userRepo.Update(ctx, existingUser.ID, &domain.UpdateUserRequest{
			IsActive: &trueVal,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to activate user account upon approval: %w", err)
		}
		userResp = updatedUser.ToResponse()
		passwordSent = "[Your Registered Password]"
	} else {
		// Generate random password and create new user account
		randomPassword, err := utils.GenerateRandomPassword(10)
		if err != nil {
			return nil, fmt.Errorf("failed to generate random password: %w", err)
		}
		passwordSent = randomPassword

		createUserReq := &domain.CreateUserRequest{
			Name:     accessReq.Name,
			Email:    accessReq.Email,
			Phone:    accessReq.Phone,
			Password: randomPassword,
		}

		createdUserResp, err := s.userService.Register(ctx, createUserReq)
		if err != nil {
			return nil, fmt.Errorf("failed to create user account upon approval: %w", err)
		}

		// Activate newly created user account
		updatedUser, err := s.userRepo.Update(ctx, createdUserResp.ID, &domain.UpdateUserRequest{
			IsActive: &trueVal,
		})
		if err == nil {
			userResp = updatedUser.ToResponse()
		} else {
			userResp = createdUserResp
			userResp.IsActive = true
		}
	}

	// Update status to APPROVED
	adminNotes := "Approved by Admin"
	if dto != nil && dto.AdminNotes != "" {
		adminNotes = dto.AdminNotes
	}
	_, _ = s.repo.UpdateStatus(ctx, objectID, domain.AccessStatusApproved, adminNotes)

	// Referral Reward Hook: Check if a pending referral exists for this email, complete it, and add 30 days validity
	if s.referralRepo != nil {
		pendingRef, _ := s.referralRepo.GetPendingByReferredEmail(ctx, accessReq.Email)
		if pendingRef != nil {
			_ = s.referralRepo.UpdateStatus(ctx, pendingRef.ID, domain.ReferralStatusCompleted, 30)
			_ = s.userRepo.ExtendValidity(ctx, pendingRef.ReferrerID, 30)
		}
	}

	// Send HTML email with credentials/approval notice via Resend API
	if s.emailSvc != nil {
		_ = s.emailSvc.SendCredentialsEmail(ctx, accessReq.Email, accessReq.Name, passwordSent)
	}

	return userResp, nil
}

// RejectRequest rejects a client access request and sends an email with the rejection reason.
func (s *accessRequestService) RejectRequest(ctx context.Context, id string, dto *domain.RejectAccessRequestDTO) (*domain.AccessRequest, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid access request ID format")
	}

	accessReq, err := s.repo.FindByID(ctx, objectID)
	if err != nil {
		return nil, err
	}

	reason := "Request rejected by Admin"
	if dto != nil && dto.Reason != "" {
		reason = dto.Reason
	}

	updatedReq, err := s.repo.UpdateStatus(ctx, objectID, domain.AccessStatusRejected, reason)
	if err != nil {
		return nil, err
	}

	// Send notification email with exact rejection reason
	if s.emailSvc != nil {
		_ = s.emailSvc.SendRejectionEmail(ctx, accessReq.Email, accessReq.Name, reason)
	}

	return updatedReq, nil
}
