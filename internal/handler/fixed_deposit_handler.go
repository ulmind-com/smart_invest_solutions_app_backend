package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// FixedDepositHandler handles HTTP requests for Fixed Deposit / Postal instrument management.
type FixedDepositHandler struct {
	service domain.FixedDepositService
}

// NewFixedDepositHandler creates a new instance of FixedDepositHandler.
func NewFixedDepositHandler(service domain.FixedDepositService) *FixedDepositHandler {
	return &FixedDepositHandler{service: service}
}

// CreateFD handles adding a new Fixed Deposit.
// @Summary      Add Fixed Deposit
// @Description  Creates a new Fixed Deposit / Postal instrument mapped to a specific family member (1st Holder). Clients create FDs under their own account; admin/super_admin may target any client by passing user_id in the body.
// @Tags         Fixed Deposit
// @Accept       json
// @Produce      json
// @Param        request  body      domain.CreateFixedDepositDTO  true  "Fixed Deposit Details"
// @Success      201      {object}  response.APIResponse{data=domain.FixedDeposit}  "Fixed Deposit added successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Security     BearerAuth
// @Router       /fixed-deposits [post]
func (h *FixedDepositHandler) CreateFD(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var dto domain.CreateFixedDepositDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	fd, err := h.service.CreateFD(c.Request.Context(), claims.Role, claims.UserID.Hex(), &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Created(c, "Fixed Deposit added successfully", fd)
}

// GetFDs handles fetching Fixed Deposits — a client's own list, or the full paginated Admin
// master list (with optional is_mapped filter) for admin/super_admin.
// @Summary      Get Fixed Deposits
// @Description  Clients receive their own Fixed Deposits (unpaginated list + total). Admin/super_admin receive a paginated master list across all clients (page, limit, is_mapped query params), each row enriched with the customer's name and contact number.
// @Tags         Fixed Deposit
// @Accept       json
// @Produce      json
// @Param        page       query     int   false  "Page number — Admin only (default: 1)"
// @Param        limit      query     int   false  "Items per page — Admin only (default: 10, max: 100)"
// @Param        is_mapped  query     bool  false  "Filter by mapped status — Admin only"
// @Success      200        {object}  response.APIResponse  "Fixed Deposits retrieved successfully"
// @Failure      401        {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /fixed-deposits [get]
func (h *FixedDepositHandler) GetFDs(c *gin.Context) {
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

		fds, total, err := h.service.GetAllFDs(c.Request.Context(), page, limit, isMapped)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, err.Error())
			return
		}

		response.SuccessWithPagination(c, "Fixed Deposits retrieved successfully", fds, page, limit, total)
		return
	}

	respData, err := h.service.GetMyFDs(c.Request.Context(), claims.UserID.Hex())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Fixed Deposits retrieved successfully", respData)
}

// GetByID handles retrieving a single Fixed Deposit by ID.
// @Summary      Get Fixed Deposit by ID
// @Description  Retrieves detailed info for a single Fixed Deposit. Clients can only access their own; admin/super_admin can access any.
// @Tags         Fixed Deposit
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Fixed Deposit ID (MongoDB ObjectID)"
// @Success      200  {object}  response.APIResponse{data=domain.FixedDeposit}  "Fixed Deposit retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      404  {object}  response.APIResponse  "Fixed Deposit not found or access denied"
// @Security     BearerAuth
// @Router       /fixed-deposits/{id} [get]
func (h *FixedDepositHandler) GetByID(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	fd, err := h.service.GetFDByID(c.Request.Context(), claims.Role, claims.UserID.Hex(), idStr)
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}

	response.Success(c, "Fixed Deposit retrieved successfully", fd)
}

// UpdateFD handles updating an existing Fixed Deposit.
// @Summary      Update / Edit Fixed Deposit
// @Description  Updates FD fields. Clients can only update their own FDs; admin/super_admin can update any. The is_mapped admin-tracking flag can ONLY be changed by admin/super_admin — if a client includes it in the request body, it is silently ignored and the existing value is preserved.
// @Tags         Fixed Deposit
// @Accept       json
// @Produce      json
// @Param        id       path      string                        true  "Fixed Deposit ID"
// @Param        request  body      domain.UpdateFixedDepositDTO  true  "Fields to update"
// @Success      200      {object}  response.APIResponse{data=domain.FixedDeposit}  "Fixed Deposit updated successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      404      {object}  response.APIResponse  "Fixed Deposit not found or access denied"
// @Security     BearerAuth
// @Router       /fixed-deposits/{id} [put]
func (h *FixedDepositHandler) UpdateFD(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	var dto domain.UpdateFixedDepositDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	fd, err := h.service.UpdateFD(c.Request.Context(), claims.Role, claims.UserID.Hex(), idStr, &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Fixed Deposit updated successfully", fd)
}

// DeleteFD handles deleting a Fixed Deposit.
// @Summary      Delete Fixed Deposit
// @Description  Removes a Fixed Deposit. Clients can only delete their own; admin/super_admin can delete any.
// @Tags         Fixed Deposit
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Fixed Deposit ID"
// @Success      200  {object}  response.APIResponse  "Fixed Deposit deleted successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request or access denied"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /fixed-deposits/{id} [delete]
func (h *FixedDepositHandler) DeleteFD(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	if err := h.service.DeleteFD(c.Request.Context(), claims.Role, claims.UserID.Hex(), idStr); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Fixed Deposit deleted successfully", nil)
}
