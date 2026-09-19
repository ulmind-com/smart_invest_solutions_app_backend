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
	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"
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

// resolveAgencyID validates a raw, user-typed Agency ID against a real admin/super_admin account
// and returns its canonical AdminID. An empty input resolves to "" (no agency) with no error; a
// non-empty input that doesn't match any admin account is rejected outright.
func (s *accessRequestService) resolveAgencyID(ctx context.Context, rawAgencyID string) (string, error) {
	return resolveAgencyAdminID(ctx, s.userRepo, rawAgencyID)
}

// SubmitRequest handles client access request submission. When an Agency ID is supplied, it must
// match a real admin/super_admin account's AdminID — an unresolvable Agency ID fails the request
// outright rather than being silently stored, so a typo never strands a request nobody can see.
func (s *accessRequestService) SubmitRequest(ctx context.Context, dto *domain.CreateAccessRequestDTO) (*domain.AccessRequest, error) {
	emailClean := strings.ToLower(strings.TrimSpace(dto.Email))
	if emailClean == "" {
		return nil, fmt.Errorf("email address is required")
	}

	agencyID, err := s.resolveAgencyID(ctx, dto.AgencyID)
	if err != nil {
		return nil, err
	}

	// Check if user already exists
	existingUser, _ := s.userRepo.FindByEmail(ctx, emailClean)
	if existingUser != nil {
		return nil, fmt.Errorf("an account with email %s already exists. Please login directly using your User ID and Security PIN", dto.Email)
	}

	// Check if an access request already exists for this email
	existingReq, _ := s.repo.FindByEmail(ctx, emailClean)
	if existingReq != nil {
		if existingReq.Status == domain.AccessStatusPending {
			return nil, fmt.Errorf("an access request for email %s is already pending approval by Admin", dto.Email)
		}
		if existingReq.Status == domain.AccessStatusApproved {
			return nil, fmt.Errorf("an access request for email %s was already approved. Please login directly", dto.Email)
		}
		// If previously REJECTED, update details & reset to PENDING instead of creating a duplicate document that fails unique index!
		updatedReq, err := s.repo.UpdateDetailsAndStatus(ctx, existingReq.ID, dto.Name, dto.Phone, dto.Notes, normalizeReferralCode(dto.AppliedReferralCode), agencyID, domain.AccessStatusPending)
		if err != nil {
			return nil, fmt.Errorf("failed to update access request: %w", err)
		}
		// A resubmission can carry a referral code the first attempt didn't.
		recordPendingReferral(ctx, s.referralRepo, s.userRepo, dto.AppliedReferralCode, emailClean)
		return updatedReq, nil
	}

	accessReq := &domain.AccessRequest{
		Name:                dto.Name,
		Email:               emailClean,
		Phone:               dto.Phone,
		Notes:               dto.Notes,
		AppliedReferralCode: normalizeReferralCode(dto.AppliedReferralCode),
		AppliedAgencyID:     agencyID,
		Status:              domain.AccessStatusPending,
	}

	createdReq, err := s.repo.Create(ctx, accessReq)
	if err != nil {
		return nil, err
	}

	// Referral tracking hook: file a Pending lead against the referrer (no-op for a bad code).
	recordPendingReferral(ctx, s.referralRepo, s.userRepo, dto.AppliedReferralCode, emailClean)

	return createdReq, nil
}

// GetAllRequests retrieves access requests with optional status filter & pagination, scoped to the
// caller: a super_admin sees every request; a plain admin sees only requests whose AppliedAgencyID
// matches their own AdminID (unassigned requests are never shown to a plain admin).
func (s *accessRequestService) GetAllRequests(ctx context.Context, requesterRole, requesterID, status string, page, limit int64) ([]*domain.AccessRequest, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
	if requesterRole == domain.RoleAdmin && agencyFilter == "" {
		// Fail closed, exactly like UserService.GetAll: an admin whose own agency can't be
		// resolved must never fall through to the platform-wide (super_admin) view.
		return []*domain.AccessRequest{}, 0, nil
	}

	return s.repo.FindAll(ctx, status, agencyFilter, page, limit)
}

// GetRequestByID retrieves a single request by ID. A plain admin gets a "not found" error (rather
// than 403) for a request outside their agency, so cross-agency existence is never revealed.
func (s *accessRequestService) GetRequestByID(ctx context.Context, requesterRole, requesterID, id string) (*domain.AccessRequest, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid access request ID format")
	}
	req, err := s.repo.FindByID(ctx, objectID)
	if err != nil {
		return nil, err
	}

	agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
	if !canAccessAgencyScopedRecord(requesterRole, agencyFilter, req.AppliedAgencyID) {
		return nil, fmt.Errorf("access request not found")
	}

	return req, nil
}

// ApproveRequest approves a client access request, activates user account (IsActive = true),
// sends credentials/approval email, and executes referral reward hook. A plain admin may only
// approve a request tied to their own Agency ID.
func (s *accessRequestService) ApproveRequest(ctx context.Context, requesterRole, requesterID, id string, dto *domain.ApproveAccessRequestDTO) (*domain.UserResponse, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid access request ID format")
	}

	accessReq, err := s.repo.FindByID(ctx, objectID)
	if err != nil {
		return nil, err
	}

	agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
	if !canAccessAgencyScopedRecord(requesterRole, agencyFilter, accessReq.AppliedAgencyID) {
		return nil, fmt.Errorf("access request not found")
	}

	adminNotes := "Approved by Admin"
	if dto != nil && dto.AdminNotes != "" {
		adminNotes = dto.AdminNotes
	}

	// Atomically claim the approval slot before doing anything else: if this request was already
	// approved (including by a concurrent call that raced us here), this fails immediately and
	// nothing below — account creation, PIN issuance, referral reward — ever runs a second time.
	if _, err := s.repo.ClaimApproval(ctx, objectID, adminNotes); err != nil {
		return nil, err
	}

	userResp, pinSent, err := s.activateApprovedAccount(ctx, accessReq)
	if err != nil {
		// Nothing was issued: put the request back in the queue rather than leaving it marked
		// "approved" with no working login behind it.
		if revertErr := s.repo.RevertApproval(ctx, objectID); revertErr != nil {
			log.Error().Err(revertErr).Str("request_id", id).Msg("failed to revert access request approval")
		}
		return nil, err
	}

	// Send approval email with User ID (Email) and 4-digit Security PIN
	if s.emailSvc != nil {
		go func() {
			if err := s.emailSvc.SendCredentialsEmail(context.Background(), accessReq.Email, accessReq.Name, pinSent); err != nil {
				log.Error().Err(err).Str("email", accessReq.Email).Msg("failed to send access approval credentials email")
			}
		}()
	}

	// Referral Reward Hook: Check if a pending referral exists for this email, complete it, and add 30 days validity
	if s.referralRepo != nil {
		pendingRef, _ := s.referralRepo.GetPendingByReferredEmail(ctx, accessReq.Email)
		if pendingRef != nil {
			_ = s.referralRepo.UpdateStatus(ctx, pendingRef.ID, domain.ReferralStatusCompleted, 30)
			_ = s.userRepo.ExtendValidity(ctx, pendingRef.ReferrerID, 30)
		}
	}

	return userResp, nil
}

// RejectRequest rejects a client access request and sends an email with the rejection reason. A
// plain admin may only reject a request tied to their own Agency ID.
func (s *accessRequestService) RejectRequest(ctx context.Context, requesterRole, requesterID, id string, dto *domain.RejectAccessRequestDTO) (*domain.AccessRequest, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid access request ID format")
	}

	accessReq, err := s.repo.FindByID(ctx, objectID)
	if err != nil {
		return nil, err
	}

	agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
	if !canAccessAgencyScopedRecord(requesterRole, agencyFilter, accessReq.AppliedAgencyID) {
		return nil, fmt.Errorf("access request not found")
	}

	reason := "Request rejected by Admin"
	if dto != nil && dto.Reason != "" {
		reason = dto.Reason
	}

	updatedReq, err := s.repo.ClaimRejection(ctx, objectID, reason)
	if err != nil {
		return nil, err
	}

	// Send notification email with exact rejection reason
	if s.emailSvc != nil {
		if err := s.emailSvc.SendRejectionEmail(ctx, accessReq.Email, accessReq.Name, reason); err != nil {
			log.Error().Err(err).Str("email", accessReq.Email).Msg("failed to send access rejection email")
		}
	}

	return updatedReq, nil
}

// activateApprovedAccount creates (or activates) the client account behind an approved request and
// issues it a fresh Security PIN, returning the account and the plaintext PIN to email.
func (s *accessRequestService) activateApprovedAccount(ctx context.Context, accessReq *domain.AccessRequest) (*domain.UserResponse, string, error) {
	generatedPIN, err := utils.GenerateNumericCode(4)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate a security PIN, please retry: %w", err)
	}

	hashedPIN, err := bcrypt.GenerateFromPassword([]byte(generatedPIN), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", fmt.Errorf("failed to hash security PIN: %w", err)
	}

	trueVal := true
	existingUser, _ := s.userRepo.FindByEmail(ctx, accessReq.Email)

	if existingUser != nil {
		// Only a client-side account that is still in play may be activated this way — never an
		// admin (that would overwrite their PIN) and never an account retired by a family merge.
		if existingUser.Role != domain.RoleClient && existingUser.Role != domain.RoleAdvisor {
			return nil, "", fmt.Errorf("this email belongs to a staff account and cannot be approved as a client")
		}
		if existingUser.MergedIntoUserID != nil {
			return nil, "", fmt.Errorf("this email belongs to an account that was merged into another family account")
		}

		// Issue a PIN & activate the account. This sets the PIN field specifically (never the
		// password) so an existing self-signup user's own chosen password is never overwritten.
		if err := s.userRepo.UpdatePIN(ctx, existingUser.ID, string(hashedPIN)); err != nil {
			return nil, "", fmt.Errorf("failed to issue a security PIN: %w", err)
		}
		updateReq := &domain.UpdateUserRequest{
			IsActive:        &trueVal,
			IsEmailVerified: &trueVal,
		}
		if accessReq.AppliedAgencyID != "" {
			updateReq.AgencyID = &accessReq.AppliedAgencyID
		}
		updatedUser, err := s.userRepo.Update(ctx, existingUser.ID, updateReq)
		if err != nil {
			return nil, "", fmt.Errorf("failed to activate user account upon approval: %w", err)
		}
		return updatedUser.ToResponse(), generatedPIN, nil
	}

	newUser := &domain.User{
		Name:  accessReq.Name,
		Email: accessReq.Email,
		Phone: accessReq.Phone,
		// PIN only — no Password is set here, since this account never chose one; it signs in
		// with the emailed PIN via the same interchangeable PIN/Password login check.
		PIN:                string(hashedPIN),
		Role:               domain.RoleClient,
		IsActive:           true,
		IsEmailVerified:    true,
		ReferralCode:       generateUniqueReferralCode(ctx, s.userRepo),
		AgencyID:           accessReq.AppliedAgencyID,
		AppValidityEndDate: time.Now().UTC().AddDate(1, 0, 0),
	}

	createdUser, err := s.userRepo.Create(ctx, newUser)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create user account upon approval: %w", err)
	}
	return createdUser.ToResponse(), generatedPIN, nil
}
