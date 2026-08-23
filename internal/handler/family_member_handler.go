package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// FamilyMemberHandler handles HTTP requests for family member management.
type FamilyMemberHandler struct {
	service domain.FamilyMemberService
}

// NewFamilyMemberHandler creates a new instance of FamilyMemberHandler.
func NewFamilyMemberHandler(service domain.FamilyMemberService) *FamilyMemberHandler {
	return &FamilyMemberHandler{
		service: service,
	}
}

// AddMember handles adding a new family member under the authenticated user (HOF).
// @Summary      Add a new family member
// @Description  Creates a new family member record linked to the authenticated user.
// @Tags         Family Members
// @Accept       json
// @Produce      json
// @Param        request  body      domain.CreateFamilyMemberDTO  true  "Family Member Details"
// @Success      201      {object}  response.APIResponse{data=domain.FamilyMember}  "Family member added successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Security     BearerAuth
// @Router       /family-members [post]
func (h *FamilyMemberHandler) AddMember(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var dto domain.CreateFamilyMemberDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	member, err := h.service.AddMember(c.Request.Context(), userIDStr, &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Created(c, "Family member added successfully", member)
}

// GetMyMembers handles retrieving all family members belonging to the authenticated user.
// @Summary      Get all my family members
// @Description  Retrieves list of all family members added by the authenticated user along with total count N.
// @Tags         Family Members
// @Accept       json
// @Produce      json
// @Success      200  {object}  response.APIResponse{data=domain.FamilyMemberListResponse}  "Family members retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /family-members [get]
func (h *FamilyMemberHandler) GetMyMembers(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	respData, err := h.service.GetMyMembers(c.Request.Context(), userIDStr)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Family members retrieved successfully", respData)
}

// GetByID handles retrieving a single family member by ID.
// @Summary      Get family member by ID
// @Description  Retrieves detailed info for a single family member.
// @Tags         Family Members
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Family Member ID (MongoDB ObjectID)"
// @Success      200  {object}  response.APIResponse{data=domain.FamilyMember}  "Family member retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      404  {object}  response.APIResponse  "Family member not found"
// @Security     BearerAuth
// @Router       /family-members/{id} [get]
func (h *FamilyMemberHandler) GetByID(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	member, err := h.service.GetMemberByID(c.Request.Context(), idStr, userIDStr)
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}

	response.Success(c, "Family member retrieved successfully", member)
}

// UpdateMember handles updating an existing family member record.
// @Summary      Update / Edit family member
// @Description  Updates specific details (name, relation, phone, email, blood group, DOB) of a family member.
// @Tags         Family Members
// @Accept       json
// @Produce      json
// @Param        id       path      string                        true  "Family Member ID"
// @Param        request  body      domain.UpdateFamilyMemberDTO  true  "Fields to update"
// @Success      200      {object}  response.APIResponse{data=domain.FamilyMember}  "Family member updated successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      404      {object}  response.APIResponse  "Family member not found"
// @Security     BearerAuth
// @Router       /family-members/{id} [put]
func (h *FamilyMemberHandler) UpdateMember(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	var dto domain.UpdateFamilyMemberDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	member, err := h.service.UpdateMember(c.Request.Context(), idStr, userIDStr, &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Family member updated successfully", member)
}

// DeleteMember handles deleting a family member record.
// @Summary      Delete family member
// @Description  Removes a family member from the authenticated user's account.
// @Tags         Family Members
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Family Member ID"
// @Success      200  {object}  response.APIResponse  "Family member deleted successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /family-members/{id} [delete]
func (h *FamilyMemberHandler) DeleteMember(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	if err := h.service.DeleteMember(c.Request.Context(), idStr, userIDStr); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Family member deleted successfully", nil)
}

// GetMembersByUserIDAdmin handles fetching family members of a client for Admin portal.
// @Summary      Get user's family members (Admin Only)
// @Description  Retrieves all family members associated with a specific user ID for Admin/Advisor view.
// @Tags         Family Members (Admin)
// @Accept       json
// @Produce      json
// @Param        userId  path      string  true  "Target Client User ID"
// @Success      200     {object}  response.APIResponse{data=domain.FamilyMemberListResponse}  "Family members retrieved successfully"
// @Failure      401     {object}  response.APIResponse  "Unauthorized"
// @Failure      403     {object}  response.APIResponse  "Forbidden — Admin role required"
// @Security     BearerAuth
// @Router       /family-members/user/{userId} [get]
func (h *FamilyMemberHandler) GetMembersByUserIDAdmin(c *gin.Context) {
	targetUserIDStr := c.Param("userId")

	respData, err := h.service.GetMembersByUserIDAdmin(c.Request.Context(), targetUserIDStr)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Family members retrieved successfully", respData)
}
