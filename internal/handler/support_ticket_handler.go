package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// SupportTicketHandler handles HTTP requests for the unified Support Ticket module.
type SupportTicketHandler struct {
	service domain.SupportTicketService
}

// NewSupportTicketHandler creates a new instance of SupportTicketHandler.
func NewSupportTicketHandler(service domain.SupportTicketService) *SupportTicketHandler {
	return &SupportTicketHandler{service: service}
}

// CreateTicket handles opening a new support ticket.
// @Summary      Open a Support Ticket
// @Description  Creates a new support ticket (Consultation, ClaimSupport, HelpDesk, or AppSupport). Clients open tickets under their own account; admin/super_admin may target any client by passing user_id in the body. A unique ticket_number (e.g. "TKT-10023") is auto-generated.
// @Tags         Support Tickets
// @Accept       json
// @Produce      json
// @Param        request  body      domain.CreateSupportTicketDTO  true  "Support Ticket Details"
// @Success      201      {object}  response.APIResponse{data=domain.SupportTicket}  "Support ticket created successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      422      {object}  response.APIResponse  "Validation error"
// @Security     BearerAuth
// @Router       /tickets [post]
func (h *SupportTicketHandler) CreateTicket(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var dto domain.CreateSupportTicketDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	ticket, err := h.service.CreateTicket(c.Request.Context(), claims.Role, claims.UserID.Hex(), &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Created(c, "Support ticket created successfully", ticket)
}

// GetTickets handles fetching support tickets — a client's own list, or the full paginated Admin
// master list (with optional status/category filters) for admin/super_admin.
// @Summary      Get Support Tickets
// @Description  Clients receive their own tickets (optionally filtered by status/category). Admin/super_admin receive a paginated master list across all clients (page, limit, status, category query params), each row enriched with the customer's name and contact number.
// @Tags         Support Tickets
// @Accept       json
// @Produce      json
// @Param        page      query     int     false  "Page number — Admin only (default: 1)"
// @Param        limit     query     int     false  "Items per page — Admin only (default: 10, max: 100)"
// @Param        status    query     string  false  "Filter by status (Open, In_Progress, Resolved, Closed)"
// @Param        category  query     string  false  "Filter by category (Consultation, ClaimSupport, HelpDesk, AppSupport)"
// @Success      200       {object}  response.APIResponse  "Tickets retrieved successfully"
// @Failure      401       {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /tickets [get]
func (h *SupportTicketHandler) GetTickets(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	status := c.Query("status")
	category := c.Query("category")

	if claims.Role == domain.RoleAdmin || claims.Role == domain.RoleSuperAdmin {
		page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
		limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "10"), 10, 64)

		tickets, total, err := h.service.GetAllTickets(c.Request.Context(), page, limit, status, category)
		if err != nil {
			response.Error(c, http.StatusBadRequest, err.Error())
			return
		}

		response.SuccessWithPagination(c, "Tickets retrieved successfully", tickets, page, limit, total)
		return
	}

	respData, err := h.service.GetMyTickets(c.Request.Context(), claims.UserID.Hex(), status, category)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Tickets retrieved successfully", respData)
}

// GetByID handles retrieving a single support ticket by ID.
// @Summary      Get Support Ticket by ID
// @Description  Retrieves detailed info for a single support ticket. Clients can only access their own; admin/super_admin can access any.
// @Tags         Support Tickets
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Support Ticket ID (MongoDB ObjectID)"
// @Success      200  {object}  response.APIResponse{data=domain.SupportTicket}  "Ticket retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      404  {object}  response.APIResponse  "Ticket not found or access denied"
// @Security     BearerAuth
// @Router       /tickets/{id} [get]
func (h *SupportTicketHandler) GetByID(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	ticket, err := h.service.GetTicketByID(c.Request.Context(), claims.Role, claims.UserID.Hex(), idStr)
	if err != nil {
		response.Error(c, http.StatusNotFound, err.Error())
		return
	}

	response.Success(c, "Ticket retrieved successfully", ticket)
}

// UpdateTicket handles updating an existing support ticket.
// @Summary      Update Support Ticket
// @Description  Updates a ticket. Clients can only update their own tickets, and can only change subject/description plus close the ticket (status=Closed) — admin_notes and any other status transition are silently ignored for clients. Admin/super_admin can update any ticket, including admin_notes and full status transitions.
// @Tags         Support Tickets
// @Accept       json
// @Produce      json
// @Param        id       path      string                          true  "Ticket ID"
// @Param        request  body      domain.UpdateSupportTicketDTO  true  "Fields to update"
// @Success      200      {object}  response.APIResponse{data=domain.SupportTicket}  "Ticket updated successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request"
// @Failure      401      {object}  response.APIResponse  "Unauthorized"
// @Failure      404      {object}  response.APIResponse  "Ticket not found or access denied"
// @Security     BearerAuth
// @Router       /tickets/{id} [put]
func (h *SupportTicketHandler) UpdateTicket(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	var dto domain.UpdateSupportTicketDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.ValidationError(c, err.Error())
		return
	}

	ticket, err := h.service.UpdateTicket(c.Request.Context(), claims.Role, claims.UserID.Hex(), idStr, &dto)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Ticket updated successfully", ticket)
}

// DeleteTicket handles deleting a support ticket. Restricted to super_admin at the router level.
// @Summary      Delete Support Ticket
// @Description  Permanently removes a support ticket. Super_admin only.
// @Tags         Support Tickets
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Ticket ID"
// @Success      200  {object}  response.APIResponse  "Ticket deleted successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — super_admin only"
// @Security     BearerAuth
// @Router       /tickets/{id} [delete]
func (h *SupportTicketHandler) DeleteTicket(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	idStr := c.Param("id")

	if err := h.service.DeleteTicket(c.Request.Context(), claims.Role, claims.UserID.Hex(), idStr); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Ticket deleted successfully", nil)
}
