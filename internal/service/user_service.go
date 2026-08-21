package service

import (
	"context"
	"fmt"
	"strconv"

	"github.com/smart-invest-solutions/backend/internal/config"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/email"
	"github.com/smart-invest-solutions/backend/pkg/utils"
	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"
)

// userService implements domain.UserService.
type userService struct {
	userRepo             domain.UserRepository
	config               *config.Config
	emailSvc             email.EmailService
	familyMemberRepo     domain.FamilyMemberRepository
	generalInsuranceRepo domain.GeneralInsuranceRepository
	documentRepo         domain.DocumentRepository
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
func (s *userService) SetCascadeDependencies(familyMemberRepo domain.FamilyMemberRepository, generalInsuranceRepo domain.GeneralInsuranceRepository, documentRepo domain.DocumentRepository, storageSvc StorageService) {
	s.familyMemberRepo = familyMemberRepo
	s.generalInsuranceRepo = generalInsuranceRepo
	s.documentRepo = documentRepo
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

// Login authenticates a user and generates a JWT token after verifying active status.
func (s *userService) Login(ctx context.Context, req *domain.LoginRequest) (*domain.LoginResponse, error) {
	// Find user by email
	user, err := s.userRepo.FindByEmail(ctx, req.Email)
	if err != nil || user == nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Check if user is active (Admin verified)
	if !user.IsActive {
		return nil, fmt.Errorf("your account is pending verification by Admin. You will receive an email once approved.")
	}

	// Compare passwords
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password))
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Parse expiry hours from config
	expiryHours, err := strconv.Atoi(s.config.JWTExpiryHours)
	if err != nil || expiryHours <= 0 {
		expiryHours = 24 // default fallback
	}

	// Generate JWT
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
func (s *userService) Update(ctx context.Context, id string, req *domain.UpdateUserRequest) (*domain.UserResponse, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	existingUser, _ := s.userRepo.FindByID(ctx, objectID)

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

// Delete removes a user by their ID.
func (s *userService) Delete(ctx context.Context, id string) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("invalid user ID format: %w", err)
	}

	return s.userRepo.Delete(ctx, objectID)
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

	// 1. Purge all user E-Vault documents from Cloudinary and delete database records
	if s.documentRepo != nil {
		docs, _, _ := s.documentRepo.FindAllByUserID(ctx, objectID, "")
		for _, doc := range docs {
			if doc.PublicID != "" && s.storageSvc != nil {
				_ = s.storageSvc.DeleteImage(ctx, doc.PublicID)
			}
		}
		_ = s.documentRepo.DeleteAllByUserID(ctx, objectID)
	}

	// 2. Cascade delete family members
	if s.familyMemberRepo != nil {
		_ = s.familyMemberRepo.DeleteAllByUserID(ctx, objectID)
	}

	// 3. Cascade delete general insurance records
	if s.generalInsuranceRepo != nil {
		_ = s.generalInsuranceRepo.DeleteAllByUserID(ctx, objectID)
	}

	// 4. Delete user profile document from MongoDB
	err = s.userRepo.Delete(ctx, objectID)
	if err != nil {
		return fmt.Errorf("failed to delete user account: %w", err)
	}

	// 5. Send account deletion notification email asynchronously
	if s.emailSvc != nil {
		go func() {
			_ = s.emailSvc.SendAccountDeletionEmail(context.Background(), user.Email, user.Name)
		}()
	}

	return nil
}
