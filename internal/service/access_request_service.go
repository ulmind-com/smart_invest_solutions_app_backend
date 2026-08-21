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
	repo        domain.AccessRequestRepository
	userRepo    domain.UserRepository
	userService domain.UserService
	emailSvc    email.EmailService
}

// NewAccessRequestService creates a new AccessRequest service instance.
func NewAccessRequestService(
	repo domain.AccessRequestRepository,
	userRepo domain.UserRepository,
	userService domain.UserService,
	emailSvc email.EmailService,
) domain.AccessRequestService {
	return &accessRequestService{
		repo:        repo,
		userRepo:    userRepo,
		userService: userService,
		emailSvc:    emailSvc,
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
		Name:   dto.Name,
		Email:  dto.Email,
		Phone:  dto.Phone,
		Notes:  dto.Notes,
		Status: domain.AccessStatusPending,
	}

	return s.repo.Create(ctx, accessReq)
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

// ApproveRequest approves a client access request, generates credentials, creates user, and emails client.
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

	// Generate strong random password
	randomPassword, err := utils.GenerateRandomPassword(10)
	if err != nil {
		return nil, fmt.Errorf("failed to generate random password: %w", err)
	}

	// Create user account with 'client' role
	createUserReq := &domain.CreateUserRequest{
		Name:     accessReq.Name,
		Email:    accessReq.Email,
		Phone:    accessReq.Phone,
		Password: randomPassword,
		Role:     domain.RoleClient,
	}

	userResp, err := s.userService.Register(ctx, createUserReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create user account upon approval: %w", err)
	}

	// Update status to APPROVED
	adminNotes := "Approved by Admin"
	if dto != nil && dto.AdminNotes != "" {
		adminNotes = dto.AdminNotes
	}
	_, _ = s.repo.UpdateStatus(ctx, objectID, domain.AccessStatusApproved, adminNotes)

	// Send HTML email with credentials via Resend API
	if s.emailSvc != nil {
		_ = s.emailSvc.SendCredentialsEmail(ctx, accessReq.Email, accessReq.Name, randomPassword)
	}

	return userResp, nil
}

// RejectRequest rejects a client access request.
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

	// Send notification email
	if s.emailSvc != nil {
		_ = s.emailSvc.SendRejectionEmail(ctx, accessReq.Email, accessReq.Name, reason)
	}

	return updatedReq, nil
}
