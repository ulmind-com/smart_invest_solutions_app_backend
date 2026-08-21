package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// AccessRequestHandler handles HTTP requests for Access Requests.
type AccessRequestHandler struct {
	service domain.AccessRequestService
}

// NewAccessRequestHandler creates a new AccessRequest handler instance.
func NewAccessRequestHandler(service domain.AccessRequestService) *AccessRequestHandler {
	return &AccessRequestHandler{
		service: service,
	}
}

// SubmitRequest handles client access request submission.
// @Summary      Request for Access (Client Sign up request)
// @Description  Submits a request for access to the platform with Name, Contact No, and Email. Status is set to PENDING until approved by an Admin.
// @Tags         Access Requests
// @Accept       json
// @Produce      json
// @Param        request  body      domain.CreateAccessRequestDTO  true  "Access Request Payload"
// @Success      201      {object}  response.APIResponse{data=domain.AccessRequest}  "Access request submitted successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request (email already exists or pending request exists)"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Router       /access-requests [post]
func (h *AccessRequestHandler) SubmitRequest(c *gin.Context) {
	var dto domain.CreateAccessRequestDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	result, err := h.service.SubmitRequest(c.Request.Context(), &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Created(c, "Access request submitted successfully. You will receive an email with your credentials upon Admin approval.", result)
}

// GetAllRequests handles listing access requests for admin.
// @Summary      Get all Access Requests (Admin Only)
// @Description  Retrieves all access requests with optional filtering by status (PENDING, APPROVED, REJECTED).
// @Tags         Access Requests (Admin)
// @Accept       json
// @Produce      json
// @Param        status  query     string  false  "Filter by status: PENDING, APPROVED, REJECTED"
// @Param        page    query     int     false  "Page number (default: 1)"
// @Param        limit   query     int     false  "Page size (default: 10)"
// @Success      200     {object}  response.PaginatedResponse{data=[]domain.AccessRequest}  "Access requests retrieved successfully"
// @Failure      401     {object}  response.APIResponse  "Unauthorized — Bearer Token required"
// @Failure      403     {object}  response.APIResponse  "Forbidden — Admin role required"
// @Security     BearerAuth
// @Router       /access-requests [get]
func (h *AccessRequestHandler) GetAllRequests(c *gin.Context) {
	status := c.Query("status")
	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "10"), 10, 64)

	requests, total, err := h.service.GetAllRequests(c.Request.Context(), status, page, limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.SuccessWithPagination(c, "Access requests retrieved successfully", requests, page, limit, total)
}

// GetRequestByID handles retrieving a single request by ID.
// @Summary      Get Access Request by ID (Admin Only)
// @Description  Retrieves detailed info for a single access request.
// @Tags         Access Requests (Admin)
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Access Request ID (MongoDB ObjectID)"
// @Success      200  {object}  response.APIResponse{data=domain.AccessRequest}  "Access request retrieved successfully"
// @Failure      404  {object}  response.APIResponse  "Request not found"
// @Security     BearerAuth
// @Router       /access-requests/{id} [get]
func (h *AccessRequestHandler) GetRequestByID(c *gin.Context) {
	id := c.Param("id")

	req, err := h.service.GetRequestByID(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}

	response.Success(c, "Access request retrieved successfully", req)
}

// ApproveRequest handles admin approval of an access request.
// @Summary      Approve Access Request & Email Credentials (Admin Only)
// @Description  Approves a client's access request. Automatically generates a secure random password, creates a User account, updates request status to APPROVED, and emails login credentials to the client via Resend.
// @Tags         Access Requests (Admin)
// @Accept       json
// @Produce      json
// @Param        id       path      string                          true  "Access Request ID"
// @Param        request  body      domain.ApproveAccessRequestDTO  false "Optional Admin Notes"
// @Success      200      {object}  response.APIResponse{data=domain.UserResponse}  "Access request approved, account created, and credentials emailed"
// @Failure      400      {object}  response.APIResponse  "Bad request (already approved or invalid ID)"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      403      {object}  response.APIResponse  "Forbidden — Admin role required"
// @Security     BearerAuth
// @Router       /access-requests/{id}/approve [post]
func (h *AccessRequestHandler) ApproveRequest(c *gin.Context) {
	id := c.Param("id")

	var dto domain.ApproveAccessRequestDTO
	_ = c.ShouldBindJSON(&dto)

	userResp, err := h.service.ApproveRequest(c.Request.Context(), id, &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Access request approved! Account created and credentials successfully emailed to client.", userResp)
}

// RejectRequest handles admin rejection of an access request.
// @Summary      Reject Access Request (Admin Only)
// @Description  Rejects a client's access request and marks status as REJECTED.
// @Tags         Access Requests (Admin)
// @Accept       json
// @Produce      json
// @Param        id       path      string                         true  "Access Request ID"
// @Param        request  body      domain.RejectAccessRequestDTO  false "Optional Rejection Reason"
// @Success      200      {object}  response.APIResponse{data=domain.AccessRequest}  "Access request rejected"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      403      {object}  response.APIResponse  "Forbidden — Admin role required"
// @Security     BearerAuth
// @Router       /access-requests/{id}/reject [post]
func (h *AccessRequestHandler) RejectRequest(c *gin.Context) {
	id := c.Param("id")

	var dto domain.RejectAccessRequestDTO
	_ = c.ShouldBindJSON(&dto)

	req, err := h.service.RejectRequest(c.Request.Context(), id, &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Access request rejected", req)
}
