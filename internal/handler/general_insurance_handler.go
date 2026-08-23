package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// GeneralInsuranceHandler handles HTTP requests for General Insurance management.
type GeneralInsuranceHandler struct {
	service domain.GeneralInsuranceService
}

// NewGeneralInsuranceHandler creates a new instance of GeneralInsuranceHandler.
func NewGeneralInsuranceHandler(service domain.GeneralInsuranceService) *GeneralInsuranceHandler {
	return &GeneralInsuranceHandler{
		service: service,
	}
}

// AddInsurance handles adding a new General Insurance policy.
// @Summary      Add General Insurance policy
// @Description  Creates a new general/vehicle insurance policy linked to the authenticated user account.
// @Tags         General Insurance
// @Accept       json
// @Produce      json
// @Param        request  body      domain.CreateGeneralInsuranceDTO  true  "General Insurance Policy Details"
// @Success      201      {object}  response.APIResponse{data=domain.GeneralInsurance}  "General Insurance policy added successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Security     BearerAuth
// @Router       /general-insurances [post]
func (h *GeneralInsuranceHandler) AddInsurance(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var dto domain.CreateGeneralInsuranceDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	policy, err := h.service.AddInsurance(c.Request.Context(), userIDStr, &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Created(c, "General Insurance policy added successfully", policy)
}

// GetMyInsurances handles retrieving all general insurance policies belonging to the authenticated user.
// @Summary      Get all my General Insurance policies
// @Description  Retrieves list of all general/vehicle insurance policies for the authenticated user.
// @Tags         General Insurance
// @Accept       json
// @Produce      json
// @Success      200  {object}  response.APIResponse{data=domain.GeneralInsuranceListResponse}  "General insurance policies retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /general-insurances [get]
func (h *GeneralInsuranceHandler) GetMyInsurances(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	respData, err := h.service.GetMyInsurances(c.Request.Context(), userIDStr)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "General insurance policies retrieved successfully", respData)
}

// GetByID handles retrieving a single general insurance policy by ID.
// @Summary      Get General Insurance policy by ID
// @Description  Retrieves detailed info for a single general insurance policy.
// @Tags         General Insurance
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "General Insurance ID (MongoDB ObjectID)"
// @Success      200  {object}  response.APIResponse{data=domain.GeneralInsurance}  "Policy retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      404  {object}  response.APIResponse  "Policy not found"
// @Security     BearerAuth
// @Router       /general-insurances/{id} [get]
func (h *GeneralInsuranceHandler) GetByID(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	policy, err := h.service.GetInsuranceByID(c.Request.Context(), idStr, userIDStr)
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}

	response.Success(c, "Policy retrieved successfully", policy)
}

// UpdateInsurance handles updating an existing general insurance policy.
// @Summary      Update / Edit General Insurance policy
// @Description  Updates vehicle no, policy no, date of expiry, company name, or advisor info.
// @Tags         General Insurance
// @Accept       json
// @Produce      json
// @Param        id       path      string                            true  "Policy ID"
// @Param        request  body      domain.UpdateGeneralInsuranceDTO  true  "Fields to update"
// @Success      200      {object}  response.APIResponse{data=domain.GeneralInsurance}  "Policy updated successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      404      {object}  response.APIResponse  "Policy not found"
// @Security     BearerAuth
// @Router       /general-insurances/{id} [put]
func (h *GeneralInsuranceHandler) UpdateInsurance(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	var dto domain.UpdateGeneralInsuranceDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	policy, err := h.service.UpdateInsurance(c.Request.Context(), idStr, userIDStr, &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Policy updated successfully", policy)
}

// DeleteInsurance handles deleting a general insurance policy.
// @Summary      Delete General Insurance policy
// @Description  Removes a general insurance policy from the authenticated user account.
// @Tags         General Insurance
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Policy ID"
// @Success      200  {object}  response.APIResponse  "Policy deleted successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /general-insurances/{id} [delete]
func (h *GeneralInsuranceHandler) DeleteInsurance(c *gin.Context) {
	userIDStr, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	if err := h.service.DeleteInsurance(c.Request.Context(), idStr, userIDStr); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Policy deleted successfully", nil)
}

// GetInsurancesByUserIDAdmin handles fetching general insurance policies of a client for Admin portal.
// @Summary      Get user's General Insurance policies (Admin Only)
// @Description  Retrieves all general insurance policies associated with a specific user ID for Admin/Advisor view.
// @Tags         General Insurance (Admin)
// @Accept       json
// @Produce      json
// @Param        userId  path      string  true  "Target Client User ID"
// @Success      200     {object}  response.APIResponse{data=domain.GeneralInsuranceListResponse}  "Policies retrieved successfully"
// @Failure      401     {object}  response.APIResponse  "Unauthorized"
// @Failure      403     {object}  response.APIResponse  "Forbidden — Admin role required"
// @Security     BearerAuth
// @Router       /general-insurances/user/{userId} [get]
func (h *GeneralInsuranceHandler) GetInsurancesByUserIDAdmin(c *gin.Context) {
	targetUserIDStr := c.Param("userId")

	respData, err := h.service.GetInsurancesByUserIDAdmin(c.Request.Context(), targetUserIDStr)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Policies retrieved successfully", respData)
}

// GetAllInsurancesAdmin handles fetching a paginated master list of every client's General
// Insurance policy for the Admin dashboard — each row enriched with the customer's name and
// contact number, so Admin can see at a glance which client holds which policy/vehicle/company.
// @Summary      Get all clients' General Insurance policies (Admin Only)
// @Description  Retrieves a paginated master list of every general/vehicle insurance policy across all clients — Customer Name, Contact No, Vehicle No, Policy No, Date of Expiry, Company Name — for the Admin dashboard. Accessible by admin and super_admin.
// @Tags         General Insurance (Admin)
// @Accept       json
// @Produce      json
// @Param        page   query     int  false  "Page number (default: 1)"
// @Param        limit  query     int  false  "Items per page (default: 10, max: 100)"
// @Success      200    {object}  response.PaginatedResponse{data=[]domain.GeneralInsuranceWithCustomer}  "Policies retrieved successfully"
// @Failure      401    {object}  response.APIResponse  "Unauthorized"
// @Failure      403    {object}  response.APIResponse  "Forbidden — Admin role required"
// @Security     BearerAuth
// @Router       /general-insurances/all [get]
func (h *GeneralInsuranceHandler) GetAllInsurancesAdmin(c *gin.Context) {
	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "10"), 10, 64)

	policies, total, err := h.service.GetAllInsurancesAdmin(c.Request.Context(), page, limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.SuccessWithPagination(c, "Policies retrieved successfully", policies, page, limit, total)
}
