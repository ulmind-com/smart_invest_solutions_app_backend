package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// HealthInsuranceHandler handles HTTP requests for Health Insurance policy management.
type HealthInsuranceHandler struct {
	service domain.HealthInsuranceService
}

// NewHealthInsuranceHandler creates a new instance of HealthInsuranceHandler.
func NewHealthInsuranceHandler(service domain.HealthInsuranceService) *HealthInsuranceHandler {
	return &HealthInsuranceHandler{service: service}
}

// CreatePolicy handles adding a new Health Insurance policy.
// @Summary      Add Health Insurance policy
// @Description  Creates a new health insurance policy mapped to a specific family member (primary insured). Clients create policies under their own account; admin/super_admin may target any client by passing user_id in the body.
// @Tags         Health Insurance
// @Accept       json
// @Produce      json
// @Param        request  body      domain.CreateHealthInsuranceDTO  true  "Health Insurance Policy Details"
// @Success      201      {object}  response.APIResponse{data=domain.HealthInsurance}  "Health Insurance policy added successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Security     BearerAuth
// @Router       /health-insurances [post]
func (h *HealthInsuranceHandler) CreatePolicy(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var dto domain.CreateHealthInsuranceDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	policy, err := h.service.CreatePolicy(c.Request.Context(), claims.Role, claims.UserID.Hex(), &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Created(c, "Health Insurance policy added successfully", policy)
}

// GetPolicies handles fetching Health Insurance policies — a client's own list, or the full
// paginated Admin master list (with optional is_mapped filter) for admin/super_admin.
// @Summary      Get Health Insurance policies
// @Description  Clients receive their own policies (unpaginated list + total). Admin/super_admin receive a paginated master list across all clients (page, limit, is_mapped query params), each row enriched with the customer's name and contact number.
// @Tags         Health Insurance
// @Accept       json
// @Produce      json
// @Param        page       query     int   false  "Page number — Admin only (default: 1)"
// @Param        limit      query     int   false  "Items per page — Admin only (default: 10, max: 100)"
// @Param        is_mapped  query     bool  false  "Filter by mapped status — Admin only"
// @Success      200        {object}  response.APIResponse  "Policies retrieved successfully"
// @Failure      401        {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /health-insurances [get]
func (h *HealthInsuranceHandler) GetPolicies(c *gin.Context) {
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

// GetByID handles retrieving a single Health Insurance policy by ID.
// @Summary      Get Health Insurance policy by ID
// @Description  Retrieves detailed info for a single health insurance policy. Clients can only access their own; admin/super_admin can access any.
// @Tags         Health Insurance
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Health Insurance Policy ID (MongoDB ObjectID)"
// @Success      200  {object}  response.APIResponse{data=domain.HealthInsurance}  "Policy retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      404  {object}  response.APIResponse  "Policy not found or access denied"
// @Security     BearerAuth
// @Router       /health-insurances/{id} [get]
func (h *HealthInsuranceHandler) GetByID(c *gin.Context) {
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

// UpdatePolicy handles updating an existing Health Insurance policy.
// @Summary      Update / Edit Health Insurance policy
// @Description  Updates policy/premium fields. Clients can only update their own policies; admin/super_admin can update any. The is_mapped admin-tracking flag can ONLY be changed by admin/super_admin — if a client includes it in the request body, it is silently ignored and the existing value is preserved. Reassigning family_member_id re-validates ownership and refreshes the cached primary_insured_name.
// @Tags         Health Insurance
// @Accept       json
// @Produce      json
// @Param        id       path      string                            true  "Policy ID"
// @Param        request  body      domain.UpdateHealthInsuranceDTO  true  "Fields to update"
// @Success      200      {object}  response.APIResponse{data=domain.HealthInsurance}  "Policy updated successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      404      {object}  response.APIResponse  "Policy not found or access denied"
// @Security     BearerAuth
// @Router       /health-insurances/{id} [put]
func (h *HealthInsuranceHandler) UpdatePolicy(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	var dto domain.UpdateHealthInsuranceDTO
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

// DeletePolicy handles deleting a Health Insurance policy.
// @Summary      Delete Health Insurance policy
// @Description  Removes a health insurance policy. Clients can only delete their own; admin/super_admin can delete any.
// @Tags         Health Insurance
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Policy ID"
// @Success      200  {object}  response.APIResponse  "Policy deleted successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request or access denied"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /health-insurances/{id} [delete]
func (h *HealthInsuranceHandler) DeletePolicy(c *gin.Context) {
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
