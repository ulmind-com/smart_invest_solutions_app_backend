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

// Login handles user authentication.
// @Summary      Login user
// @Description  Authenticates a user with email and password. Returns a JWT token containing the user's role (client/advisor/admin/super_admin). The frontend should use the role to redirect to the correct dashboard.
// @Tags         Authentication
// @Accept       json
// @Produce      json
// @Param        request  body      domain.LoginRequest  true  "Login credentials"
// @Success      200      {object}  response.APIResponse{data=domain.LoginResponse}  "Login successful — returns JWT token and user details with role"
// @Failure      401      {object}  response.APIResponse  "Invalid credentials or account disabled"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Router       /users/login [post]
func (h *UserHandler) Login(c *gin.Context) {
	var req domain.LoginRequest
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
// @Description  Updates user details (name, email, phone, role). Requires authentication.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        id       path      string                    true  "User ID (MongoDB ObjectID)"
// @Param        request  body      domain.UpdateUserRequest   true  "Fields to update"
// @Success      200      {object}  response.APIResponse{data=domain.UserResponse}  "User updated successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Security     BearerAuth
// @Router       /users/{id} [put]
func (h *UserHandler) Update(c *gin.Context) {
	id := c.Param("id")

	var req domain.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	user, err := h.userService.Update(c.Request.Context(), id, &req)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "User updated successfully", user)
}

// Delete handles deleting a user.
// @Summary      Delete user (Admin only)
// @Description  Deletes a user by ID. Only accessible by admin and super_admin roles.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "User ID (MongoDB ObjectID)"
// @Success      200  {object}  response.APIResponse  "User deleted successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — admin role required"
// @Security     BearerAuth
// @Router       /users/{id} [delete]
func (h *UserHandler) Delete(c *gin.Context) {
	id := c.Param("id")

	if err := h.userService.Delete(c.Request.Context(), id); err != nil {
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
	userIDVal, exists := c.Get(middleware.AuthCtxKey)
	if !exists {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID := userIDVal.(string)

	user, err := h.userService.GetByID(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}

	response.Success(c, "Profile retrieved successfully", user)
}

// UpdateProfile handles updating the authenticated user's profile details.
// @Summary      Update user profile
// @Description  Updates authenticated user's name and contact phone number. Email is immutable and cannot be modified.
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
	userIDVal, exists := c.Get(middleware.AuthCtxKey)
	if !exists {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID := userIDVal.(string)

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
	userIDVal, exists := c.Get(middleware.AuthCtxKey)
	if !exists {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID := userIDVal.(string)

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
