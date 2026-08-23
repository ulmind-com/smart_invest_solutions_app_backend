package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// DashboardHandler handles HTTP requests for aggregated dashboard views.
type DashboardHandler struct {
	service domain.DashboardService
}

// NewDashboardHandler creates a new instance of DashboardHandler.
func NewDashboardHandler(service domain.DashboardService) *DashboardHandler {
	return &DashboardHandler{service: service}
}

// GetClientDashboard handles fetching the authenticated client's aggregated dashboard summary.
// @Summary      Get client dashboard
// @Description  Returns the authenticated client's aggregated summary: total family members, total policies/FDs across every module, and Life/Health premiums due within the next 30 days. Client role only.
// @Tags         Dashboard
// @Accept       json
// @Produce      json
// @Success      200  {object}  response.APIResponse{data=domain.ClientDashboardDTO}  "Dashboard retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — client role required"
// @Security     BearerAuth
// @Router       /dashboard/client [get]
func (h *DashboardHandler) GetClientDashboard(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	dashboard, err := h.service.GetClientDashboard(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Dashboard retrieved successfully", dashboard)
}

// GetAdminDashboard handles fetching the platform-wide aggregated dashboard summary.
// @Summary      Get admin dashboard
// @Description  Returns platform-wide aggregated totals: active clients, pending access requests, and mapped/unmapped policy counts across Life, Health, General Insurance, and Fixed Deposits. Admin/super_admin only.
// @Tags         Dashboard
// @Accept       json
// @Produce      json
// @Success      200  {object}  response.APIResponse{data=domain.AdminDashboardDTO}  "Dashboard retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — admin role required"
// @Security     BearerAuth
// @Router       /dashboard/admin [get]
func (h *DashboardHandler) GetAdminDashboard(c *gin.Context) {
	dashboard, err := h.service.GetAdminDashboard(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Dashboard retrieved successfully", dashboard)
}
