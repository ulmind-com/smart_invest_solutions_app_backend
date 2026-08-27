package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// UserHandler handles HTTP requests for user operations.
type UserHandler struct {
	userService      domain.UserService
	passResetService domain.PasswordResetService
}

// NewUserHandler creates a new user handler.
func NewUserHandler(userService domain.UserService, passResetService domain.PasswordResetService) *UserHandler {
	return &UserHandler{
		userService:      userService,
		passResetService: passResetService,
	}
}

// Register handles user registration.
// @Summary      Register a new user
// @Description  Creates a new user account with the specified details. Default role is 'client'.
// @Tags         Authentication
// @Accept       json
// @Produce      json
// @Param        request  body      domain.CreateUserRequest  true  "User registration details"
// @Success      201      {object}  response.APIResponse{data=domain.UserResponse}  "User registered successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request (email already exists, etc.)"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Router       /users/register [post]
func (h *UserHandler) Register(c *gin.Context) {
	var req domain.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	user, err := h.userService.Register(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Created(c, "User registered successfully", user)
}

// Login handles authentication for normal users (client, advisor) using Email + Password.
// Admin/super_admin accounts are rejected here — they must use POST /admins/login instead.
// @Summary      Login (client/advisor)
// @Description  Authenticates a normal user (client or advisor) using registered Email + Password. Admin/super_admin accounts cannot log in through this endpoint — use POST /admins/login instead. Returns a JWT token containing the user's role. Accounts lock for 15 minutes after 5 consecutive failed attempts.
// @Tags         Authentication
// @Accept       json
// @Produce      json
// @Param        request  body      domain.UserLoginRequest  true  "Login credentials — registered Email and Password"
// @Success      200      {object}  response.APIResponse{data=domain.LoginResponse}  "Login successful — returns JWT token and user details with role"
// @Failure      401      {object}  response.APIResponse  "Invalid credentials, account disabled, or account temporarily locked"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Router       /users/login [post]
func (h *UserHandler) Login(c *gin.Context) {
	var req domain.UserLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	loginResp, err := h.userService.Login(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	response.Success(c, "Login successful", loginResp)
}

// AdminLogin handles authentication for admin/super_admin accounts using AdminID + PIN.
// @Summary      Login (admin/super_admin)
// @Description  Authenticates an admin or super_admin account using AdminID + PIN. Normal users (client/advisor) cannot log in through this endpoint — use POST /users/login instead. Returns a JWT token containing the user's role. Accounts lock for 15 minutes after 5 consecutive failed attempts.
// @Tags         Authentication
// @Accept       json
// @Produce      json
// @Param        request  body      domain.AdminLoginRequest  true  "Login credentials — Admin ID and 4-digit PIN"
// @Success      200      {object}  response.APIResponse{data=domain.LoginResponse}  "Login successful — returns JWT token and user details with role"
// @Failure      401      {object}  response.APIResponse  "Invalid credentials, account disabled, or account temporarily locked"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Router       /admins/login [post]
func (h *UserHandler) AdminLogin(c *gin.Context) {
	var req domain.AdminLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	loginResp, err := h.userService.AdminLogin(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	response.Success(c, "Login successful", loginResp)
}

// GetByID handles fetching a user by ID.
// @Summary      Get user by ID
// @Description  Retrieves a user's details by their MongoDB ObjectID. Requires authentication.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "User ID (MongoDB ObjectID)"
// @Success      200  {object}  response.APIResponse{data=domain.UserResponse}  "User retrieved successfully"
// @Failure      404  {object}  response.APIResponse  "User not found"
// @Security     BearerAuth
// @Router       /users/{id} [get]
func (h *UserHandler) GetByID(c *gin.Context) {
	id := c.Param("id")

	user, err := h.userService.GetByID(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}

	response.Success(c, "User retrieved successfully", user)
}

// GetAll handles fetching all users with pagination.
// @Summary      Get all users (Admin only)
// @Description  Retrieves a paginated list of all users. Only accessible by admin and super_admin roles.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        page   query     int  false  "Page number (default: 1)"
// @Param        limit  query     int  false  "Items per page (default: 10, max: 100)"
// @Success      200    {object}  response.PaginatedResponse{data=[]domain.UserResponse}  "Users retrieved successfully"
// @Failure      401    {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Failure      403    {object}  response.APIResponse  "Forbidden — admin role required"
// @Failure      500    {object}  response.APIResponse  "Internal server error"
// @Security     BearerAuth
// @Router       /users [get]
func (h *UserHandler) GetAll(c *gin.Context) {
	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "10"), 10, 64)

	users, total, err := h.userService.GetAll(c.Request.Context(), page, limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.SuccessWithPagination(c, "Users retrieved successfully", users, page, limit, total)
}

// Update handles updating a user.
// @Summary      Update user
// @Description  Updates user details (name, email, phone, role). Requires authentication. Only a super_admin may modify an existing admin/super_admin account or assign the admin/super_admin role.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        id       path      string                    true  "User ID (MongoDB ObjectID)"
// @Param        request  body      domain.UpdateUserRequest   true  "Fields to update"
// @Success      200      {object}  response.APIResponse{data=domain.UserResponse}  "User updated successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      403      {object}  response.APIResponse  "Forbidden — only super_admin can modify admin accounts or assign admin roles"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Security     BearerAuth
// @Router       /users/{id} [put]
func (h *UserHandler) Update(c *gin.Context) {
	id := c.Param("id")

	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req domain.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	user, err := h.userService.Update(c.Request.Context(), claims.Role, id, &req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "User updated successfully", user)
}

// Delete handles deleting a user.
// @Summary      Delete user (Admin only)
// @Description  Deletes a user by ID. Only accessible by admin and super_admin roles. Only a super_admin may delete an existing admin/super_admin account.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "User ID (MongoDB ObjectID)"
// @Success      200  {object}  response.APIResponse  "User deleted successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — admin role required, or only super_admin can delete admin accounts"
// @Security     BearerAuth
// @Router       /users/{id} [delete]
func (h *UserHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.userService.Delete(c.Request.Context(), claims.Role, id); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "User deleted successfully", nil)
}

// GetProfile handles fetching the authenticated user's profile.
// @Summary      Get current user profile
// @Description  Retrieves profile information for the currently authenticated user.
// @Tags         Profile
// @Accept       json
// @Produce      json
// @Success      200  {object}  response.APIResponse{data=domain.UserResponse}  "Profile retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /users/me [get]
func (h *UserHandler) GetProfile(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.userService.GetByID(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}

	response.Success(c, "Profile retrieved successfully", user)
}

// UpdateProfile handles updating the authenticated user's profile details.
// @Summary      Update user profile
// @Description  Updates authenticated user's name and contact phone number. Works for every role (client, advisor, admin, super_admin) — an admin can update their own name/phone the same way. Email is immutable and cannot be modified by anyone through this endpoint.
// @Tags         Profile
// @Accept       json
// @Produce      json
// @Param        request  body      domain.UpdateProfileRequest  true  "Profile fields to update"
// @Success      200      {object}  response.APIResponse{data=domain.UserResponse}  "Profile updated successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Security     BearerAuth
// @Router       /users/me [put]
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req domain.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	user, err := h.userService.UpdateProfile(c.Request.Context(), userID, &req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Profile updated successfully", user)
}

// DeleteMyAccount handles deleting the logged-in user's own account.
// @Summary      Delete logged-in user account
// @Description  Permanently deletes the authenticated user's account and wipes all associated E-Vault documents (from Cloudinary), family details, and insurance policies. Not available for admin/super_admin accounts — an admin account can only be deleted by a super_admin via DELETE /admins/{id}.
// @Tags         Profile
// @Accept       json
// @Produce      json
// @Success      200  {object}  response.APIResponse  "Account permanently deleted"
// @Failure      400  {object}  response.APIResponse  "Admin/super_admin accounts cannot be self-deleted"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      500  {object}  response.APIResponse  "Failed to delete account"
// @Security     BearerAuth
// @Router       /users/me [delete]
func (h *UserHandler) DeleteMyAccount(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.userService.DeleteMyAccount(c.Request.Context(), userID); err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Account and associated data permanently deleted successfully", nil)
}

// ChangePassword handles changing the authenticated user's password.
// @Summary      Change password
// @Description  Changes the authenticated user's password after validating current password.
// @Tags         Profile
// @Accept       json
// @Produce      json
// @Param        request  body      domain.ChangePasswordRequest  true  "Password change payload"
// @Success      200      {object}  response.APIResponse  "Password changed successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request or current password mismatch"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Security     BearerAuth
// @Router       /users/change-password [put]
func (h *UserHandler) ChangePassword(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req domain.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	if err := h.userService.ChangePassword(c.Request.Context(), userID, &req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Password changed successfully", nil)
}

// ForgotPassword handles requesting a password reset OTP.
// @Summary      Request password reset OTP
// @Description  Sends a 6-digit OTP code to the registered email address if an account exists.
// @Tags         Authentication
// @Accept       json
// @Produce      json
// @Param        request  body      domain.ForgotPasswordRequest  true  "Registered Email Address"
// @Success      200      {object}  response.APIResponse  "If an account exists with this email, an OTP has been sent."
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Router       /users/forgot-password [post]
func (h *UserHandler) ForgotPassword(c *gin.Context) {
	var req domain.ForgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	_ = h.passResetService.SendOTP(c.Request.Context(), &req)
	response.Success(c, "If an account exists with this email, an OTP has been sent.", nil)
}

// VerifyOTP handles verifying the 6-digit OTP code.
// @Summary      Verify password reset OTP
// @Description  Validates if the provided 6-digit OTP code is valid, unexpired, and unused.
// @Tags         Authentication
// @Accept       json
// @Produce      json
// @Param        request  body      domain.VerifyOTPRequest  true  "Email & 6-digit OTP"
// @Success      200      {object}  response.APIResponse  "OTP verified successfully"
// @Failure      400      {object}  response.APIResponse  "Invalid or expired OTP code"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Router       /users/verify-otp [post]
func (h *UserHandler) VerifyOTP(c *gin.Context) {
	var req domain.VerifyOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	if err := h.passResetService.VerifyOTP(c.Request.Context(), &req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "OTP verified successfully", nil)
}

// ResetPassword handles setting a new password using a valid OTP code.
// @Summary      Reset password using OTP
// @Description  Verifies the OTP code and updates the user's password in the database.
// @Tags         Authentication
// @Accept       json
// @Produce      json
// @Param        request  body      domain.ResetPasswordRequest  true  "Email, OTP & New Password payload"
// @Success      200      {object}  response.APIResponse  "Password reset successfully"
// @Failure      400      {object}  response.APIResponse  "Invalid OTP, password mismatch or weak password"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Router       /users/reset-password [post]
func (h *UserHandler) ResetPassword(c *gin.Context) {
	var req domain.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	if err := h.passResetService.ResetPassword(c.Request.Context(), &req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Password reset successfully", nil)
}

// CreateAdmin handles creating a new Admin account. Super Admin only.
// @Summary      Create Admin account (Super Admin only)
// @Description  Creates a new Admin account from Name/Email/Phone. Auto-generates a unique Admin ID, a random password, and a 4-digit PIN, then emails the credentials to the new admin. The response also returns the plaintext password/PIN once, so the Super Admin can share them even if the email fails to deliver. Only accessible by super_admin.
// @Tags         Admin Accounts
// @Accept       json
// @Produce      json
// @Param        request  body      domain.CreateAdminRequest  true  "New Admin details"
// @Success      201      {object}  response.APIResponse{data=domain.CreateAdminResponse}  "Admin account created successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request (e.g. email already in use)"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      403      {object}  response.APIResponse  "Forbidden — super_admin role required"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Security     BearerAuth
// @Router       /admins [post]
func (h *UserHandler) CreateAdmin(c *gin.Context) {
	var req domain.CreateAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	result, err := h.userService.CreateAdmin(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Created(c, "Admin account created successfully", result)
}

// GetAllAdmins handles fetching all admin & super_admin accounts. Super Admin only.
// @Summary      Get all Admin accounts (Super Admin only)
// @Description  Retrieves a paginated list of all admin and super_admin accounts. Only accessible by super_admin.
// @Tags         Admin Accounts
// @Accept       json
// @Produce      json
// @Param        page   query     int  false  "Page number (default: 1)"
// @Param        limit  query     int  false  "Items per page (default: 10, max: 100)"
// @Success      200    {object}  response.PaginatedResponse{data=[]domain.UserResponse}  "Admin accounts retrieved successfully"
// @Failure      401    {object}  response.APIResponse  "Unauthorized"
// @Failure      403    {object}  response.APIResponse  "Forbidden — super_admin role required"
// @Security     BearerAuth
// @Router       /admins [get]
func (h *UserHandler) GetAllAdmins(c *gin.Context) {
	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "10"), 10, 64)

	admins, total, err := h.userService.GetAllAdmins(c.Request.Context(), page, limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.SuccessWithPagination(c, "Admin accounts retrieved successfully", admins, page, limit, total)
}

// DeleteAdmin handles permanently deleting an Admin account. Super Admin only.
// @Summary      Delete Admin account (Super Admin only)
// @Description  Permanently deletes an admin account and its associated data. Self-deletion is disallowed — use the account settings delete option instead. Only accessible by super_admin.
// @Tags         Admin Accounts
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Admin User ID (MongoDB ObjectID)"
// @Success      200  {object}  response.APIResponse  "Admin account deleted successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — super_admin role required"
// @Security     BearerAuth
// @Router       /admins/{id} [delete]
func (h *UserHandler) DeleteAdmin(c *gin.Context) {
	requesterID, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	targetID := c.Param("id")

	if err := h.userService.DeleteAdmin(c.Request.Context(), requesterID, targetID); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Admin account deleted successfully", nil)
}

// ImpersonateUser handles logging in on behalf of any target user or admin account. Super Admin only.
// @Summary      Impersonate user or admin account (Super Admin only)
// @Description  Generates a valid JWT token for a target user or admin account, allowing a Super Admin to view dashboards and perform actions on their behalf.
// @Tags         Admin Accounts
// @Accept       json
// @Produce      json
// @Param        request  body      domain.ImpersonateUserRequest  true  "Impersonation Payload"
// @Success      200      {object}  response.APIResponse{data=domain.LoginResponse}  "Successfully impersonated target account"
// @Failure      400      {object}  response.APIResponse  "Invalid request payload or target account inactive/super_admin"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      403      {object}  response.APIResponse  "Forbidden — super_admin role required"
// @Security     BearerAuth
// @Router       /admins/impersonate [post]
func (h *UserHandler) ImpersonateUser(c *gin.Context) {
	superAdminID, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	requesterRole, _ := c.Get("role")
	if requesterRole != domain.RoleSuperAdmin {
		response.Error(c, http.StatusForbidden, "only a super_admin can impersonate user accounts")
		return
	}

	var req domain.ImpersonateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	loginResp, err := h.userService.ImpersonateUser(c.Request.Context(), superAdminID, req.TargetUserID, req.Reason)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Successfully impersonated target account", loginResp)
}
