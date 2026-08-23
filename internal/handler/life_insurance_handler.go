package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// LifeInsuranceHandler handles HTTP requests for Life Insurance policy management.
type LifeInsuranceHandler struct {
	service domain.LifeInsuranceService
}

// NewLifeInsuranceHandler creates a new instance of LifeInsuranceHandler.
func NewLifeInsuranceHandler(service domain.LifeInsuranceService) *LifeInsuranceHandler {
	return &LifeInsuranceHandler{service: service}
}

// CreatePolicy handles adding a new Life Insurance policy.
// @Summary      Add Life Insurance policy
// @Description  Creates a new life insurance policy mapped to a specific family member. Clients create policies under their own account; admin/super_admin may target any client by passing user_id in the body ("Select Member" flow).
// @Tags         Life Insurance
// @Accept       json
// @Produce      json
// @Param        request  body      domain.CreateLifeInsuranceDTO  true  "Life Insurance Policy Details"
// @Success      201      {object}  response.APIResponse{data=domain.LifeInsurance}  "Life Insurance policy added successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Security     BearerAuth
// @Router       /life-insurances [post]
func (h *LifeInsuranceHandler) CreatePolicy(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var dto domain.CreateLifeInsuranceDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	policy, err := h.service.CreatePolicy(c.Request.Context(), claims.Role, claims.UserID.Hex(), &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Created(c, "Life Insurance policy added successfully", policy)
}

// GetPolicies handles fetching Life Insurance policies — a client's own list, or the full
// paginated Admin master list (with optional is_mapped filter) for admin/super_admin.
// @Summary      Get Life Insurance policies
// @Description  Clients receive their own policies (unpaginated list + total). Admin/super_admin receive a paginated master list across all clients (page, limit, is_mapped query params), each row enriched with the customer's name and contact number.
// @Tags         Life Insurance
// @Accept       json
// @Produce      json
// @Param        page       query     int   false  "Page number — Admin only (default: 1)"
// @Param        limit      query     int   false  "Items per page — Admin only (default: 10, max: 100)"
// @Param        is_mapped  query     bool  false  "Filter by mapped status — Admin only"
// @Success      200        {object}  response.APIResponse  "Policies retrieved successfully"
// @Failure      401        {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /life-insurances [get]
func (h *LifeInsuranceHandler) GetPolicies(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	if claims.Role == domain.RoleAdmin || claims.Role == domain.RoleSuperAdmin {
		page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
		limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "10"), 10, 64)

		var isMapped *bool
		if raw := c.Query("is_mapped"); raw != "" {
			parsed, err := strconv.ParseBool(raw)
			if err != nil {
				response.ValidationError(c, "is_mapped must be true or false")
				return
			}
			isMapped = &parsed
		}

		policies, total, err := h.service.GetAllPolicies(c.Request.Context(), page, limit, isMapped)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, err.Error())
			return
		}

		response.SuccessWithPagination(c, "Policies retrieved successfully", policies, page, limit, total)
		return
	}

	respData, err := h.service.GetMyPolicies(c.Request.Context(), claims.UserID.Hex())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Policies retrieved successfully", respData)
}

// GetByID handles retrieving a single Life Insurance policy by ID.
// @Summary      Get Life Insurance policy by ID
// @Description  Retrieves detailed info for a single life insurance policy. Clients can only access their own; admin/super_admin can access any.
// @Tags         Life Insurance
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Life Insurance Policy ID (MongoDB ObjectID)"
// @Success      200  {object}  response.APIResponse{data=domain.LifeInsurance}  "Policy retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      404  {object}  response.APIResponse  "Policy not found or access denied"
// @Security     BearerAuth
// @Router       /life-insurances/{id} [get]
func (h *LifeInsuranceHandler) GetByID(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	policy, err := h.service.GetPolicyByID(c.Request.Context(), claims.Role, claims.UserID.Hex(), idStr)
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}

	response.Success(c, "Policy retrieved successfully", policy)
}

// UpdatePolicy handles updating an existing Life Insurance policy.
// @Summary      Update / Edit Life Insurance policy
// @Description  Updates policy/premium fields. Clients can only update their own policies; admin/super_admin can update any. Reassigning family_member_id re-validates ownership and refreshes the cached life_insured_name.
// @Tags         Life Insurance
// @Accept       json
// @Produce      json
// @Param        id       path      string                          true  "Policy ID"
// @Param        request  body      domain.UpdateLifeInsuranceDTO  true  "Fields to update"
// @Success      200      {object}  response.APIResponse{data=domain.LifeInsurance}  "Policy updated successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      404      {object}  response.APIResponse  "Policy not found or access denied"
// @Security     BearerAuth
// @Router       /life-insurances/{id} [put]
func (h *LifeInsuranceHandler) UpdatePolicy(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	var dto domain.UpdateLifeInsuranceDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	policy, err := h.service.UpdatePolicy(c.Request.Context(), claims.Role, claims.UserID.Hex(), idStr, &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Policy updated successfully", policy)
}

// DeletePolicy handles deleting a Life Insurance policy.
// @Summary      Delete Life Insurance policy
// @Description  Removes a life insurance policy. Clients can only delete their own; admin/super_admin can delete any.
// @Tags         Life Insurance
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Policy ID"
// @Success      200  {object}  response.APIResponse  "Policy deleted successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request or access denied"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /life-insurances/{id} [delete]
func (h *LifeInsuranceHandler) DeletePolicy(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	if err := h.service.DeletePolicy(c.Request.Context(), claims.Role, claims.UserID.Hex(), idStr); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Policy deleted successfully", nil)
}
