package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

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
)

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

// SetCascadeDependencies wires repositories for full cascade account deletion.
func (s *userService) SetCascadeDependencies(familyMemberRepo domain.FamilyMemberRepository, generalInsuranceRepo domain.GeneralInsuranceRepository, documentRepo domain.DocumentRepository, lifeInsuranceRepo domain.LifeInsuranceRepository, fixedDepositRepo domain.FixedDepositRepository, healthInsuranceRepo domain.HealthInsuranceRepository, supportTicketRepo domain.SupportTicketRepository, storageSvc StorageService) {
	s.familyMemberRepo = familyMemberRepo
	s.generalInsuranceRepo = generalInsuranceRepo
	s.documentRepo = documentRepo
	s.lifeInsuranceRepo = lifeInsuranceRepo
	s.fixedDepositRepo = fixedDepositRepo
	s.healthInsuranceRepo = healthInsuranceRepo
	s.supportTicketRepo = supportTicketRepo
	s.storageSvc = storageSvc
}

// Register creates a new user with a hashed password, setting IsActive to false (pending verification) and sending a Welcome email.
func (s *userService) Register(ctx context.Context, req *domain.CreateUserRequest) (*domain.UserResponse, error) {
	// Check if user with this email already exists
	existing, _ := s.userRepo.FindByEmail(ctx, req.Email)
	if existing != nil {
		return nil, fmt.Errorf("user with email %s already exists", req.Email)
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Default role is client; account is pending Admin verification (IsActive = false)
	user := &domain.User{
		Name:     req.Name,
		Email:    req.Email,
		Password: string(hashedPassword),
		Phone:    req.Phone,
		Role:     domain.RoleClient,
		IsActive: false, // Pending Admin verification
	}

	createdUser, err := s.userRepo.Create(ctx, user)
	if err != nil {
		return nil, err
	}

	// Send automatic Welcome Email asynchronously
	if s.emailSvc != nil {
		go func() {
			_ = s.emailSvc.SendWelcomeEmail(context.Background(), createdUser.Email, createdUser.Name)
		}()
	}

	return createdUser.ToResponse(), nil
}

// Login authenticates a normal user (client/advisor) by Email + Password. Admin/super_admin
// accounts cannot sign in through this endpoint — they must use AdminLogin instead. The password
// is checked before the role gate so that a wrong-endpoint hint is only ever revealed to someone
// who already proved they know the correct password (no new information is leaked to an attacker).
func (s *userService) Login(ctx context.Context, req *domain.UserLoginRequest) (*domain.LoginResponse, error) {
	user, err := s.userRepo.FindByEmail(ctx, req.Email)
	if err != nil || user == nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Check if account is temporarily locked due to repeated failed attempts
	if user.LockedUntil != nil && user.LockedUntil.After(time.Now().UTC()) {
		remaining := time.Until(*user.LockedUntil).Round(time.Minute)
		return nil, fmt.Errorf("account temporarily locked due to multiple failed login attempts. Try again in %s", remaining)
	}

	// Check if user is active (Admin verified)
	if !user.IsActive {
		return nil, fmt.Errorf("your account is pending verification by Admin. You will receive an email once approved.")
	}

	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)) != nil {
		attempts, recErr := s.userRepo.RecordFailedLogin(ctx, user.ID)
		if recErr == nil && attempts >= maxFailedLoginAttempts {
			_ = s.userRepo.LockAccount(ctx, user.ID, time.Now().UTC().Add(accountLockDuration))
		}
		return nil, fmt.Errorf("invalid credentials")
	}

	// Admin/super_admin accounts are restricted to the AdminLogin (AdminID + PIN) flow
	if user.Role == domain.RoleAdmin || user.Role == domain.RoleSuperAdmin {
		return nil, fmt.Errorf("admin accounts must sign in with Admin ID and PIN via the admin login")
	}

	_ = s.userRepo.ClearFailedLogins(ctx, user.ID)

	return s.issueToken(user)
}

// AdminLogin authenticates an admin/super_admin account by AdminID + PIN. Normal users (client/
// advisor) have no admin_id/pin and can never match here, and non-admin roles are rejected even in
// the (unreachable in practice) case where an admin_id collision existed.
func (s *userService) AdminLogin(ctx context.Context, req *domain.AdminLoginRequest) (*domain.LoginResponse, error) {
	user, err := s.userRepo.FindByAdminID(ctx, req.AdminID)
	if err != nil || user == nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	if user.Role != domain.RoleAdmin && user.Role != domain.RoleSuperAdmin {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Check if account is temporarily locked due to repeated failed attempts
	if user.LockedUntil != nil && user.LockedUntil.After(time.Now().UTC()) {
		remaining := time.Until(*user.LockedUntil).Round(time.Minute)
		return nil, fmt.Errorf("account temporarily locked due to multiple failed login attempts. Try again in %s", remaining)
	}

	// Check if user is active (Super Admin verified)
	if !user.IsActive {
		return nil, fmt.Errorf("your account is pending verification by Admin. You will receive an email once approved.")
	}

	if user.PIN == "" || bcrypt.CompareHashAndPassword([]byte(user.PIN), []byte(req.PIN)) != nil {
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

// GetAll retrieves a paginated list of users.
func (s *userService) GetAll(ctx context.Context, page, limit int64) ([]*domain.UserResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	users, total, err := s.userRepo.FindAll(ctx, page, limit)
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
				_ = s.emailSvc.SendCredentialsEmail(context.Background(), updatedUser.Email, updatedUser.Name, "[Your Registered Password]")
			}()
		} else {
			// Account Deactivated / Rejected
			go func() {
				_ = s.emailSvc.SendRejectionEmail(context.Background(), updatedUser.Email, updatedUser.Name, "Your account has been set to inactive by Admin.")
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
			_ = s.emailSvc.SendAccountDeletionEmail(context.Background(), user.Email, user.Name)
		}()
	}

	return nil
}

// CreateAdmin creates a new Admin account (Super Admin only, enforced at the router level). It
// generates a unique Admin ID, a random password, and a 4-digit PIN, then emails the credentials
// to the new admin. The account is immediately active since a super_admin has already vetted it.
func (s *userService) CreateAdmin(ctx context.Context, req *domain.CreateAdminRequest) (*domain.CreateAdminResponse, error) {
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

	newAdmin := &domain.User{
		Name:     req.Name,
		Email:    req.Email,
		Phone:    req.Phone,
		Password: string(hashedPassword),
		PIN:      string(hashedPIN),
		AdminID:  adminID,
		Role:     domain.RoleAdmin,
		IsActive: true,
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
			_ = s.emailSvc.SendAccountDeletionEmail(context.Background(), target.Email, target.Name)
		}()
	}

	return nil
}
