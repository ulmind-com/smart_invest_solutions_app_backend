package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/config"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/email"
	"github.com/smart-invest-solutions/backend/pkg/utils"
	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"
)

// maxFailedLoginAttempts is the number of consecutive failed login attempts allowed before an
// account is temporarily locked. accountLockDuration is how long the lock lasts.
const (
	maxFailedLoginAttempts = 5
	accountLockDuration    = 15 * time.Minute
	// adminExpiryAlertCooldown limits how often a Super Admin can (re-)send an expiry-warning email
	// to the same admin, so repeatedly tapping the button in the app can't flood their inbox.
	adminExpiryAlertCooldown = 1 * time.Hour
)

// matchesSecret reports whether secret matches either the account's PIN or Password hash. PIN and
// Password are treated as interchangeable credentials throughout this service (Login, AdminLogin,
// and ChangePIN all use this), so a client can sign in — or authorize a PIN change — with whichever
// one they currently have set.
func matchesSecret(user *domain.User, secret string) bool {
	if secret == "" {
		return false
	}
	if user.PIN != "" && bcrypt.CompareHashAndPassword([]byte(user.PIN), []byte(secret)) == nil {
		return true
	}
	if user.Password != "" && bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(secret)) == nil {
		return true
	}
	return false
}

// resolveCallerAgencyID returns the AdminID of the calling account when it's a plain admin — used
// to scope agency-restricted queries (access requests, client lists, dashboard counts) to just
// their own agency. Returns "" for super_admin (meaning "no filter, see everything") and for any
// role/lookup failure, so a caller that isn't a scoped admin never accidentally gets over-filtered.
func resolveCallerAgencyID(ctx context.Context, userRepo domain.UserRepository, requesterRole, requesterID string) string {
	if requesterRole != domain.RoleAdmin {
		return ""
	}
	objID, err := bson.ObjectIDFromHex(requesterID)
	if err != nil {
		return ""
	}
	caller, err := userRepo.FindByID(ctx, objID)
	if err != nil || caller == nil {
		return ""
	}
	return caller.AdminID
}

// canAccessAgencyScopedRecord reports whether a caller may view/act on a record tied to
// recordAgencyID. A super_admin always can. A plain admin can only when it matches their own
// agency — an unassigned record (recordAgencyID == "") is visible only to a super_admin.
func canAccessAgencyScopedRecord(requesterRole, callerAgencyID, recordAgencyID string) bool {
	if requesterRole == domain.RoleSuperAdmin {
		return true
	}
	return recordAgencyID != "" && recordAgencyID == callerAgencyID
}

// checkAdminExpiry rejects login for a role=admin account whose configured AdminExpiryDate has
// passed. super_admin accounts never expire (AdminExpiryDate is never set for them), and a nil
// AdminExpiryDate on an admin account means "no expiry" (e.g. accounts created before this feature
// existed), so login proceeds unaffected in both cases.
func checkAdminExpiry(user *domain.User) error {
	if user.Role == domain.RoleAdmin && user.AdminExpiryDate != nil && user.AdminExpiryDate.Before(time.Now().UTC()) {
		return fmt.Errorf("your admin access expired on %s. Please contact your super admin to renew it", user.AdminExpiryDate.Format("02 Jan 2006"))
	}
	return nil
}

// userService implements domain.UserService.
type userService struct {
	userRepo             domain.UserRepository
	config               *config.Config
	emailSvc             email.EmailService
	familyMemberRepo     domain.FamilyMemberRepository
	generalInsuranceRepo domain.GeneralInsuranceRepository
	documentRepo         domain.DocumentRepository
	lifeInsuranceRepo    domain.LifeInsuranceRepository
	fixedDepositRepo     domain.FixedDepositRepository
	healthInsuranceRepo  domain.HealthInsuranceRepository
	supportTicketRepo    domain.SupportTicketRepository
	accessReqRepo        domain.AccessRequestRepository
	verifRepo            domain.EmailVerificationRepository
	storageSvc           StorageService
}

// NewUserService creates a new user service with the given repository, config, and email service.
func NewUserService(userRepo domain.UserRepository, cfg *config.Config, emailSvc email.EmailService) domain.UserService {
	return &userService{
		userRepo: userRepo,
		config:   cfg,
		emailSvc: emailSvc,
	}
}

// SetCascadeDependencies wires repositories for full cascade account deletion, access requests, and email verification.
func (s *userService) SetCascadeDependencies(familyMemberRepo domain.FamilyMemberRepository, generalInsuranceRepo domain.GeneralInsuranceRepository, documentRepo domain.DocumentRepository, lifeInsuranceRepo domain.LifeInsuranceRepository, fixedDepositRepo domain.FixedDepositRepository, healthInsuranceRepo domain.HealthInsuranceRepository, supportTicketRepo domain.SupportTicketRepository, accessReqRepo domain.AccessRequestRepository, verifRepo domain.EmailVerificationRepository, storageSvc StorageService) {
	s.familyMemberRepo = familyMemberRepo
	s.generalInsuranceRepo = generalInsuranceRepo
	s.documentRepo = documentRepo
	s.lifeInsuranceRepo = lifeInsuranceRepo
	s.fixedDepositRepo = fixedDepositRepo
	s.healthInsuranceRepo = healthInsuranceRepo
	s.supportTicketRepo = supportTicketRepo
	s.accessReqRepo = accessReqRepo
	s.verifRepo = verifRepo
	s.storageSvc = storageSvc
}

// Register creates a new user with a hashed password, setting IsEmailVerified to false and sending a 6-digit OTP email.
func (s *userService) Register(ctx context.Context, req *domain.CreateUserRequest) (*domain.UserResponse, error) {
	// Check if user with this email already exists
	existing, _ := s.userRepo.FindByEmail(ctx, req.Email)
	if existing != nil {
		if existing.IsEmailVerified {
			return nil, fmt.Errorf("user with email %s already exists", req.Email)
		}
		// If email is not verified yet, update existing record details so user can complete OTP verification
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash password: %w", err)
		}
		_ = s.userRepo.UpdatePassword(ctx, existing.ID, string(hashedPassword))
		name := req.Name
		phone := req.Phone
		_, _ = s.userRepo.Update(ctx, existing.ID, &domain.UpdateUserRequest{Name: &name, Phone: &phone})

		// Generate & send new 6-digit OTP
		otpCode, _ := utils.GenerateNumericCode(6)
		verifRecord := &domain.EmailVerification{
			Email:     existing.Email,
			OTP:       otpCode,
			ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
			IsUsed:    false,
			Attempts:  0,
		}
		if s.verifRepo != nil {
			_, _ = s.verifRepo.Create(ctx, verifRecord)
		}
		if s.emailSvc != nil {
			go func() {
				if err := s.emailSvc.SendVerificationOTPEmail(context.Background(), existing.Email, existing.Name, otpCode); err != nil {
					log.Error().Err(err).Str("email", existing.Email).Msg("failed to send verification OTP email")
				}
			}()
		}
		return existing.ToResponse(), nil
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Generate unique 6-character referral code
	var refCode string
	for attempt := 0; attempt < 5; attempt++ {
		candidate, _ := utils.GenerateReferralCode(6)
		if candidate != "" {
			if existingRef, _ := s.userRepo.FindByReferralCode(ctx, candidate); existingRef == nil {
				refCode = candidate
				break
			}
		}
	}
	if refCode == "" {
		refCode = "REF" + strconv.FormatInt(time.Now().UnixNano()%1000, 10)
	}

	// Default role is client; account is unverified (IsEmailVerified = false) and pending Admin verification (IsActive = false)
	user := &domain.User{
		Name:               req.Name,
		Email:              utils.NormalizeEmail(req.Email),
		Password:           string(hashedPassword),
		Phone:              req.Phone,
		Role:               domain.RoleClient,
		IsActive:           false, // Pending Admin verification
		IsEmailVerified:    false, // Pending OTP verification
		ReferralCode:       refCode,
		AppValidityEndDate: time.Now().UTC().AddDate(1, 0, 0), // Default 1 year validity
	}

	createdUser, err := s.userRepo.Create(ctx, user)
	if err != nil {
		return nil, err
	}

	// Generate 6-digit numeric verification OTP
	otpCode, err := utils.GenerateNumericCode(6)
	if err == nil {
		verifRecord := &domain.EmailVerification{
			Email:     createdUser.Email,
			OTP:       otpCode,
			ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
			IsUsed:    false,
			Attempts:  0,
		}
		if s.verifRepo != nil {
			_, _ = s.verifRepo.Create(ctx, verifRecord)
		}
		if s.emailSvc != nil {
			go func() {
				if err := s.emailSvc.SendVerificationOTPEmail(context.Background(), createdUser.Email, createdUser.Name, otpCode); err != nil {
					log.Error().Err(err).Str("email", createdUser.Email).Msg("failed to send verification OTP email")
				}
			}()
		}
	}

	return createdUser.ToResponse(), nil
}

// Login authenticates any user (client, advisor, admin, super_admin) using User ID / Admin ID / Email + PIN / Password.
func (s *userService) Login(ctx context.Context, req *domain.UserLoginRequest) (*domain.LoginResponse, error) {
	identifier := strings.TrimSpace(req.Identifier)
	if identifier == "" {
		identifier = strings.TrimSpace(req.Email)
	}
	if identifier == "" {
		identifier = strings.TrimSpace(req.AdminID)
	}
	if identifier == "" {
		identifier = strings.TrimSpace(req.UserID)
	}
	if identifier == "" {
		return nil, fmt.Errorf("user ID / email / admin ID is required")
	}

	secret := req.PIN
	if secret == "" {
		secret = req.Password
	}
	if secret == "" {
		return nil, fmt.Errorf("security PIN / password is required")
	}

	// 1. Search by AdminID
	user, err := s.userRepo.FindByAdminID(ctx, identifier)
	if err != nil || user == nil {
		// 2. Search by Email
		user, err = s.userRepo.FindByEmail(ctx, strings.ToLower(identifier))
	}
	if err != nil || user == nil {
		// 3. Search by BSON ObjectID
		if objID, errHex := bson.ObjectIDFromHex(identifier); errHex == nil {
			user, _ = s.userRepo.FindByID(ctx, objID)
		}
	}

	if user == nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Check if account is temporarily locked due to repeated failed attempts
	if user.LockedUntil != nil && user.LockedUntil.After(time.Now().UTC()) {
		remaining := time.Until(*user.LockedUntil).Round(time.Minute)
		return nil, fmt.Errorf("account temporarily locked due to multiple failed login attempts. Try again in %s", remaining)
	}

	// Check if email has been verified via OTP
	if !user.IsEmailVerified && user.Role == domain.RoleClient {
		return nil, fmt.Errorf("your email address is not verified. Please verify your email using the OTP sent to your inbox.")
	}

	// Check if user is active (Admin verified)
	if !user.IsActive {
		return nil, fmt.Errorf("your account is pending verification by Admin. You will receive an email once approved.")
	}

	// Check if this admin account's configured expiry date has passed
	if err := checkAdminExpiry(user); err != nil {
		return nil, err
	}

	// Verify secret against PIN or Password
	if !matchesSecret(user, secret) {
		attempts, recErr := s.userRepo.RecordFailedLogin(ctx, user.ID)
		if recErr == nil && attempts >= maxFailedLoginAttempts {
			_ = s.userRepo.LockAccount(ctx, user.ID, time.Now().UTC().Add(accountLockDuration))
		}
		return nil, fmt.Errorf("invalid credentials")
	}

	_ = s.userRepo.ClearFailedLogins(ctx, user.ID)

	return s.issueToken(user)
}

// AdminLogin authenticates an admin/super_admin account by AdminID or Email + PIN or Password.
func (s *userService) AdminLogin(ctx context.Context, req *domain.AdminLoginRequest) (*domain.LoginResponse, error) {
	identifier := strings.TrimSpace(req.AdminID)
	if identifier == "" {
		identifier = strings.TrimSpace(req.Email)
	}
	if identifier == "" {
		return nil, fmt.Errorf("admin ID or email is required")
	}

	user, err := s.userRepo.FindByAdminID(ctx, identifier)
	if err != nil || user == nil {
		user, err = s.userRepo.FindByEmail(ctx, strings.ToLower(identifier))
	}
	if err != nil || user == nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	if user.Role != domain.RoleAdmin && user.Role != domain.RoleSuperAdmin {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Check if account is temporarily locked
	if user.LockedUntil != nil && user.LockedUntil.After(time.Now().UTC()) {
		remaining := time.Until(*user.LockedUntil).Round(time.Minute)
		return nil, fmt.Errorf("account temporarily locked due to multiple failed login attempts. Try again in %s", remaining)
	}

	if !user.IsActive {
		return nil, fmt.Errorf("your account is pending verification by Admin. You will receive an email once approved.")
	}

	// Check if this admin account's configured expiry date has passed
	if err := checkAdminExpiry(user); err != nil {
		return nil, err
	}

	if !matchesSecret(user, req.PIN) {
		attempts, recErr := s.userRepo.RecordFailedLogin(ctx, user.ID)
		if recErr == nil && attempts >= maxFailedLoginAttempts {
			_ = s.userRepo.LockAccount(ctx, user.ID, time.Now().UTC().Add(accountLockDuration))
		}
		return nil, fmt.Errorf("invalid credentials")
	}

	_ = s.userRepo.ClearFailedLogins(ctx, user.ID)

	return s.issueToken(user)
}

// issueToken generates a JWT (carrying the user's role) and wraps it with the user's public
// profile into a LoginResponse. Shared by Login and AdminLogin.
func (s *userService) issueToken(user *domain.User) (*domain.LoginResponse, error) {
	expiryHours, err := strconv.Atoi(s.config.JWTExpiryHours)
	if err != nil || expiryHours <= 0 {
		expiryHours = 24 // default fallback
	}

	token, err := utils.GenerateJWT(user.ID, user.Role, s.config.JWTSecret, expiryHours)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	return &domain.LoginResponse{
		Token: token,
		User:  user.ToResponse(),
	}, nil
}

// ImpersonateUser allows a super_admin to issue a short-lived impersonation token for any target user or admin account without needing their password/PIN.
func (s *userService) ImpersonateUser(ctx context.Context, superAdminIDStr, targetUserIDStr, reason string) (*domain.LoginResponse, error) {
	superAdminID, err := bson.ObjectIDFromHex(superAdminIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid super admin ID format: %w", err)
	}

	targetUserIDStr = strings.TrimSpace(targetUserIDStr)
	if targetUserIDStr == "" {
		return nil, fmt.Errorf("target user ID, email, or admin ID is required")
	}

	var targetUser *domain.User
	if objID, errHex := bson.ObjectIDFromHex(targetUserIDStr); errHex == nil {
		targetUser, _ = s.userRepo.FindByID(ctx, objID)
	}
	if targetUser == nil {
		targetUser, _ = s.userRepo.FindByEmail(ctx, strings.ToLower(targetUserIDStr))
	}
	if targetUser == nil {
		targetUser, _ = s.userRepo.FindByAdminID(ctx, targetUserIDStr)
	}

	if targetUser == nil {
		return nil, fmt.Errorf("target user account not found")
	}

	if targetUser.ID.Hex() == superAdminIDStr {
		return nil, fmt.Errorf("super admin cannot impersonate themselves")
	}

	// Security Guardrail: Super admin accounts cannot be impersonated
	if targetUser.Role == domain.RoleSuperAdmin {
		return nil, fmt.Errorf("super_admin accounts cannot be impersonated")
	}

	// Target user must be active
	if !targetUser.IsActive {
		return nil, fmt.Errorf("cannot impersonate an inactive or unapproved account")
	}

	expiryHours, err := strconv.Atoi(s.config.JWTExpiryHours)
	if err != nil || expiryHours <= 0 {
		expiryHours = 24
	}

	token, err := utils.GenerateImpersonationJWT(targetUser.ID, superAdminID, targetUser.Role, s.config.JWTSecret, expiryHours)
	if err != nil {
		return nil, fmt.Errorf("failed to generate impersonation token: %w", err)
	}

	log.Info().
		Str("super_admin_id", superAdminIDStr).
		Str("target_user_id", targetUser.ID.Hex()).
		Str("target_name", targetUser.Name).
		Str("target_role", targetUser.Role).
		Str("reason", reason).
		Msg("[SECURITY AUDIT] Super Admin impersonated account")

	return &domain.LoginResponse{
		Token: token,
		User:  targetUser.ToResponse(),
	}, nil
}

// GetByID retrieves a user by their ID string.
func (s *userService) GetByID(ctx context.Context, id string) (*domain.UserResponse, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	user, err := s.userRepo.FindByID(ctx, objectID)
	if err != nil {
		return nil, err
	}

	return user.ToResponse(), nil
}

// GetAll retrieves a paginated list of users, scoped to the caller: a super_admin sees everyone
// (any role); a plain admin sees only role=client accounts whose AgencyID matches their own
// AdminID — clients who registered under a different agency, or no agency at all, never appear.
func (s *userService) GetAll(ctx context.Context, requesterRole, requesterID string, page, limit int64) ([]*domain.UserResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	var roleFilter string
	agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
	if requesterRole == domain.RoleAdmin {
		roleFilter = domain.RoleClient
		// Fail closed: a plain admin whose own AgencyID can't be resolved must never fall through
		// to the unfiltered (super_admin) view — they see nothing rather than everything.
		if agencyFilter == "" {
			return []*domain.UserResponse{}, 0, nil
		}
	}

	users, total, err := s.userRepo.FindAll(ctx, roleFilter, agencyFilter, page, limit)
	if err != nil {
		return nil, 0, err
	}

	var responses []*domain.UserResponse
	for _, user := range users {
		responses = append(responses, user.ToResponse())
	}

	return responses, total, nil
}

// Update modifies an existing user and triggers Approval / Rejection email if IsActive status changes.
// Only a super_admin may modify an existing admin/super_admin account, or promote any user to
// admin/super_admin — this closes a privilege-escalation hole where a plain admin could otherwise
// tamper with other admin accounts or self-promote via this generic endpoint.
func (s *userService) Update(ctx context.Context, requesterRole, id string, req *domain.UpdateUserRequest) (*domain.UserResponse, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	existingUser, _ := s.userRepo.FindByID(ctx, objectID)

	if requesterRole != domain.RoleSuperAdmin {
		if existingUser != nil && (existingUser.Role == domain.RoleAdmin || existingUser.Role == domain.RoleSuperAdmin) {
			return nil, fmt.Errorf("only a super_admin can modify an admin account")
		}
		if req.Role != nil && (*req.Role == domain.RoleAdmin || *req.Role == domain.RoleSuperAdmin) {
			return nil, fmt.Errorf("only a super_admin can assign the admin or super_admin role")
		}
	}

	updatedUser, err := s.userRepo.Update(ctx, objectID, req)
	if err != nil {
		return nil, err
	}

	// Trigger email notifications if Admin toggles account active status
	if existingUser != nil && req.IsActive != nil && existingUser.IsActive != *req.IsActive && s.emailSvc != nil {
		if *req.IsActive {
			// Account Verified / Approved
			go func() {
				if err := s.emailSvc.SendCredentialsEmail(context.Background(), updatedUser.Email, updatedUser.Name, "[Your Registered Password]"); err != nil {
					log.Error().Err(err).Str("email", updatedUser.Email).Msg("failed to send credentials email")
				}
			}()
		} else {
			// Account Deactivated / Rejected
			go func() {
				if err := s.emailSvc.SendRejectionEmail(context.Background(), updatedUser.Email, updatedUser.Name, "Your account has been set to inactive by Admin."); err != nil {
					log.Error().Err(err).Str("email", updatedUser.Email).Msg("failed to send account deactivation email")
				}
			}()
		}
	}

	return updatedUser.ToResponse(), nil
}

// UpdateProfile updates the profile fields (name, phone) for a user. Email is strictly ignored/immutable.
func (s *userService) UpdateProfile(ctx context.Context, id string, req *domain.UpdateProfileRequest) (*domain.UserResponse, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	updateReq := &domain.UpdateUserRequest{
		Name:  req.Name,
		Phone: req.Phone,
		// Email and Role are intentionally omitted to enforce immutability
	}

	user, err := s.userRepo.Update(ctx, objectID, updateReq)
	if err != nil {
		return nil, err
	}

	return user.ToResponse(), nil
}

// ChangePassword allows a logged-in user to change their password after verifying their current password.
func (s *userService) ChangePassword(ctx context.Context, id string, req *domain.ChangePasswordRequest) error {
	if req.NewPassword != req.ConfirmPassword {
		return fmt.Errorf("new password and confirmation password do not match")
	}

	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}

	user, err := s.userRepo.FindByID(ctx, objectID)
	if err != nil {
		return fmt.Errorf("user not found")
	}

	// Verify current password
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.CurrentPassword))
	if err != nil {
		return fmt.Errorf("current password is incorrect")
	}

	// Ensure new password is different
	if req.CurrentPassword == req.NewPassword {
		return fmt.Errorf("new password must be different from current password")
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	return s.userRepo.UpdatePassword(ctx, objectID, string(hashedPassword))
}

// ChangePIN lets a logged-in user set or change their 4-digit Security PIN. The caller must supply
// whichever credential currently protects the account — their existing PIN if they already have
// one, or their password if they don't (e.g. a client setting a PIN for the first time). This lets
// any client adopt PIN-based quick login from their profile, not just accounts that received one
// via access-request approval.
func (s *userService) ChangePIN(ctx context.Context, id string, req *domain.ChangePINRequest) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}

	user, err := s.userRepo.FindByID(ctx, objectID)
	if err != nil {
		return fmt.Errorf("user not found")
	}

	if !matchesSecret(user, req.CurrentCredential) {
		return fmt.Errorf("current PIN or password is incorrect")
	}

	if len(req.NewPIN) != 4 {
		return fmt.Errorf("PIN must be exactly 4 digits")
	}
	for _, ch := range req.NewPIN {
		if ch < '0' || ch > '9' {
			return fmt.Errorf("PIN must contain only digits")
		}
	}

	hashedPIN, err := bcrypt.GenerateFromPassword([]byte(req.NewPIN), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash new PIN: %w", err)
	}

	return s.userRepo.UpdatePIN(ctx, objectID, string(hashedPIN))
}

// Delete removes a user by their ID. Only a super_admin may delete an existing admin/super_admin account.
func (s *userService) Delete(ctx context.Context, requesterRole, id string) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}

	if requesterRole != domain.RoleSuperAdmin {
		target, _ := s.userRepo.FindByID(ctx, objectID)
		if target != nil && (target.Role == domain.RoleAdmin || target.Role == domain.RoleSuperAdmin) {
			return fmt.Errorf("only a super_admin can delete an admin account")
		}
	}

	return s.userRepo.Delete(ctx, objectID)
}

// cascadeWipeUserData purges a user's E-Vault documents (including Cloudinary assets), family members,
// general insurance records, life insurance policies, fixed deposits, health insurance policies,
// and support tickets. Shared by DeleteMyAccount and DeleteAdmin.
func (s *userService) cascadeWipeUserData(ctx context.Context, objectID bson.ObjectID) {
	if s.documentRepo != nil {
		docs, _, _ := s.documentRepo.FindAllByUserID(ctx, objectID, "")
		for _, doc := range docs {
			if doc.PublicID != "" && s.storageSvc != nil {
				_ = s.storageSvc.DeleteImage(ctx, doc.PublicID)
			}
		}
		_ = s.documentRepo.DeleteAllByUserID(ctx, objectID)
	}

	if s.familyMemberRepo != nil {
		_ = s.familyMemberRepo.DeleteAllByUserID(ctx, objectID)
	}

	if s.generalInsuranceRepo != nil {
		_ = s.generalInsuranceRepo.DeleteAllByUserID(ctx, objectID)
	}

	if s.lifeInsuranceRepo != nil {
		_ = s.lifeInsuranceRepo.DeleteAllByUserID(ctx, objectID)
	}

	if s.fixedDepositRepo != nil {
		_ = s.fixedDepositRepo.DeleteAllByUserID(ctx, objectID)
	}

	if s.healthInsuranceRepo != nil {
		_ = s.healthInsuranceRepo.DeleteAllByUserID(ctx, objectID)
	}

	if s.supportTicketRepo != nil {
		_ = s.supportTicketRepo.DeleteAllByUserID(ctx, objectID)
	}
}

// DeleteMyAccount permanently deletes the logged in user account and wipes all associated records and Cloudinary files.
func (s *userService) DeleteMyAccount(ctx context.Context, userIDStr string) error {
	objectID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}

	user, err := s.userRepo.FindByID(ctx, objectID)
	if err != nil || user == nil {
		return fmt.Errorf("user account not found")
	}

	// Admin/super_admin accounts cannot self-delete — only a super_admin can remove an admin account
	// (via DeleteAdmin), which also prevents an admin from accidentally locking everyone out.
	if user.Role == domain.RoleAdmin || user.Role == domain.RoleSuperAdmin {
		return fmt.Errorf("admin accounts cannot be deleted via self-service; please contact a super_admin to remove your access")
	}

	s.cascadeWipeUserData(ctx, objectID)

	// Delete user profile document from MongoDB
	err = s.userRepo.Delete(ctx, objectID)
	if err != nil {
		return fmt.Errorf("failed to delete user account: %w", err)
	}

	// Send account deletion notification email asynchronously
	if s.emailSvc != nil {
		go func() {
			if err := s.emailSvc.SendAccountDeletionEmail(context.Background(), user.Email, user.Name); err != nil {
				log.Error().Err(err).Str("email", user.Email).Msg("failed to send account deletion email")
			}
		}()
	}

	return nil
}

// CreateAdmin creates a new Admin account (Super Admin only, enforced at the router level). It
// generates a unique Admin ID, a random password, and a 4-digit PIN, then emails the credentials
// to the new admin. The account is immediately active since a super_admin has already vetted it.
func (s *userService) CreateAdmin(ctx context.Context, req *domain.CreateAdminRequest) (*domain.CreateAdminResponse, error) {
	if !req.ExpiryDate.After(time.Now().UTC()) {
		return nil, fmt.Errorf("expiry date must be in the future")
	}

	existing, _ := s.userRepo.FindByEmail(ctx, req.Email)
	if existing != nil {
		return nil, fmt.Errorf("a user with email %s already exists", req.Email)
	}

	// Generate a unique Admin ID (astronomically unlikely to collide, but retry defensively)
	var adminID string
	for attempt := 0; attempt < 5; attempt++ {
		candidate, genErr := utils.GenerateAdminID()
		if genErr != nil {
			return nil, fmt.Errorf("failed to generate admin ID: %w", genErr)
		}
		if existingByID, _ := s.userRepo.FindByAdminID(ctx, candidate); existingByID == nil {
			adminID = candidate
			break
		}
	}
	if adminID == "" {
		return nil, fmt.Errorf("failed to generate a unique admin ID, please retry")
	}

	password, err := utils.GenerateRandomPassword(10)
	if err != nil {
		return nil, fmt.Errorf("failed to generate password: %w", err)
	}

	pin, err := utils.GenerateNumericCode(4)
	if err != nil {
		return nil, fmt.Errorf("failed to generate PIN: %w", err)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}
	hashedPIN, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash PIN: %w", err)
	}

	expiryDate := req.ExpiryDate
	newAdmin := &domain.User{
		Name:            req.Name,
		Email:           utils.NormalizeEmail(req.Email),
		Phone:           req.Phone,
		Password:        string(hashedPassword),
		PIN:             string(hashedPIN),
		AdminID:         adminID,
		Role:            domain.RoleAdmin,
		IsActive:        true,
		AdminExpiryDate: &expiryDate,
	}

	createdAdmin, err := s.userRepo.Create(ctx, newAdmin)
	if err != nil {
		return nil, fmt.Errorf("failed to create admin account: %w", err)
	}

	// Send credentials synchronously — this is a rare, sensitive action where delivery status matters,
	// unlike the fire-and-forget pattern used for high-volume notification emails elsewhere.
	emailSent := false
	if s.emailSvc != nil {
		if sendErr := s.emailSvc.SendAdminCredentialsEmail(ctx, createdAdmin.Email, createdAdmin.Name, adminID, password, pin); sendErr == nil {
			emailSent = true
		}
	}

	return &domain.CreateAdminResponse{
		Admin:                createdAdmin.ToResponse(),
		AdminID:              adminID,
		Email:                createdAdmin.Email,
		TemporaryPassword:    password,
		TemporaryPIN:         pin,
		CredentialsEmailSent: emailSent,
	}, nil
}

// GetAllAdmins retrieves a paginated list of all admin and super_admin accounts.
func (s *userService) GetAllAdmins(ctx context.Context, page, limit int64) ([]*domain.UserResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	admins, total, err := s.userRepo.FindAllByRoles(ctx, []string{domain.RoleAdmin, domain.RoleSuperAdmin}, page, limit)
	if err != nil {
		return nil, 0, err
	}

	var responses []*domain.UserResponse
	for _, admin := range admins {
		responses = append(responses, admin.ToResponse())
	}

	return responses, total, nil
}

// DeleteAdmin permanently deletes an admin account and its associated data. Self-deletion is
// disallowed here to prevent accidental Super Admin lockout — use the /users/me flow instead.
func (s *userService) DeleteAdmin(ctx context.Context, requesterID, targetID string) error {
	if requesterID == targetID {
		return fmt.Errorf("cannot delete your own account via this endpoint; use the account settings delete option instead")
	}

	objectID, err := bson.ObjectIDFromHex(targetID)
	if err != nil {
		return fmt.Errorf("invalid admin ID format: %w", err)
	}

	target, err := s.userRepo.FindByID(ctx, objectID)
	if err != nil || target == nil {
		return fmt.Errorf("admin account not found")
	}
	if target.Role != domain.RoleAdmin && target.Role != domain.RoleSuperAdmin {
		return fmt.Errorf("target account is not an admin account")
	}

	s.cascadeWipeUserData(ctx, objectID)

	if err := s.userRepo.Delete(ctx, objectID); err != nil {
		return fmt.Errorf("failed to delete admin account: %w", err)
	}

	if s.emailSvc != nil {
		go func() {
			if err := s.emailSvc.SendAccountDeletionEmail(context.Background(), target.Email, target.Name); err != nil {
				log.Error().Err(err).Str("email", target.Email).Msg("failed to send account deletion email")
			}
		}()
	}

	return nil
}

// ListExpiringAdmins returns admin accounts (role=admin) whose expiry date is at or before
// now + withinDays, sorted soonest-first — surfaces both already-expired admins and those
// approaching their cutoff so a Super Admin can renew them, or send a warning email, in time.
func (s *userService) ListExpiringAdmins(ctx context.Context, withinDays int) ([]*domain.UserResponse, error) {
	if withinDays < 0 {
		withinDays = 0
	}
	cutoff := time.Now().UTC().AddDate(0, 0, withinDays)

	admins, err := s.userRepo.FindExpiringAdmins(ctx, cutoff)
	if err != nil {
		return nil, err
	}

	responses := make([]*domain.UserResponse, 0, len(admins))
	for _, admin := range admins {
		responses = append(responses, admin.ToResponse())
	}
	return responses, nil
}

// RenewAdminExpiry lets a Super Admin push an admin account's expiry date forward, reactivating an
// already-expired admin's ability to log in, and emails the admin a confirmation.
func (s *userService) RenewAdminExpiry(ctx context.Context, targetID string, req *domain.RenewAdminExpiryRequest) (*domain.UserResponse, error) {
	objectID, err := bson.ObjectIDFromHex(targetID)
	if err != nil {
		return nil, fmt.Errorf("invalid admin ID format: %w", err)
	}

	target, err := s.userRepo.FindByID(ctx, objectID)
	if err != nil || target == nil {
		return nil, fmt.Errorf("admin account not found")
	}
	if target.Role != domain.RoleAdmin {
		return nil, fmt.Errorf("expiry dates only apply to admin accounts, not %s accounts", target.Role)
	}
	if !req.ExpiryDate.After(time.Now().UTC()) {
		return nil, fmt.Errorf("new expiry date must be in the future")
	}

	if err := s.userRepo.UpdateAdminExpiry(ctx, objectID, req.ExpiryDate); err != nil {
		return nil, err
	}

	updated, err := s.userRepo.FindByID(ctx, objectID)
	if err != nil {
		return nil, err
	}

	if s.emailSvc != nil {
		newExpiry := req.ExpiryDate
		toEmail, toName := updated.Email, updated.Name
		go func() {
			if err := s.emailSvc.SendAdminExpiryRenewedEmail(context.Background(), toEmail, toName, newExpiry); err != nil {
				log.Error().Err(err).Str("email", toEmail).Msg("failed to send admin expiry renewal confirmation email")
			}
		}()
	}

	return updated.ToResponse(), nil
}

// SendAdminExpiryAlert emails a specific admin whose access is expiring soon (or has already
// expired), asking them to contact the Super Admin to renew it. Throttled to at most one alert per
// adminExpiryAlertCooldown window per admin so repeated taps can't flood their inbox.
func (s *userService) SendAdminExpiryAlert(ctx context.Context, targetID string) error {
	objectID, err := bson.ObjectIDFromHex(targetID)
	if err != nil {
		return fmt.Errorf("invalid admin ID format: %w", err)
	}

	target, err := s.userRepo.FindByID(ctx, objectID)
	if err != nil || target == nil {
		return fmt.Errorf("admin account not found")
	}
	if target.Role != domain.RoleAdmin {
		return fmt.Errorf("expiry alerts only apply to admin accounts")
	}
	if target.AdminExpiryDate == nil {
		return fmt.Errorf("this admin account has no expiry date set")
	}
	if target.LastExpiryAlertSentAt != nil {
		elapsed := time.Since(*target.LastExpiryAlertSentAt)
		if elapsed < adminExpiryAlertCooldown {
			wait := (adminExpiryAlertCooldown - elapsed).Round(time.Minute)
			return fmt.Errorf("an alert was already sent recently; please wait %s before sending another", wait)
		}
	}
	if s.emailSvc == nil {
		return fmt.Errorf("email service is not configured")
	}

	if err := s.emailSvc.SendAdminExpiryAlertEmail(ctx, target.Email, target.Name, *target.AdminExpiryDate); err != nil {
		return fmt.Errorf("failed to send expiry alert email: %w", err)
	}

	return s.userRepo.RecordExpiryAlertSent(ctx, objectID)
}
