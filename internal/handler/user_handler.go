package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// UserHandler handles HTTP requests for user operations.
type UserHandler struct {
	userService domain.UserService
}

// NewUserHandler creates a new user handler.
func NewUserHandler(userService domain.UserService) *UserHandler {
	return &UserHandler{
		userService: userService,
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
