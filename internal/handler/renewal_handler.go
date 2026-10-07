package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// RenewalHandler handles HTTP requests for the renewal book.
type RenewalHandler struct {
	renewalService domain.RenewalService
}

// NewRenewalHandler creates a new RenewalHandler.
func NewRenewalHandler(renewalService domain.RenewalService) *RenewalHandler {
	return &RenewalHandler{renewalService: renewalService}
}

// GetRenewals returns what is due and when, across every instrument.
// @Summary      Renewal book (Agency staff)
// @Description  Every premium due, policy expiring and deposit maturing from 90 days overdue through the next 90 days, merged across life, health, motor and deposits and ordered by urgency. Each row carries the days remaining (negative when overdue), the client who holds it, and the admin whose agency they belong to. A super_admin sees the whole platform and may narrow with `agency_id` (or `unassigned` for clients with no agency); a plain admin only ever sees their own agency. The `summary` counts the whole scope, so it does not move when `window` or `kind` change.
// @Tags         Renewals
// @Produce      json
// @Param        q          query     string  false  "Search client, title, reference number or agency"
// @Param        agency_id  query     string  false  "Super admin only: narrow to one Agency ID, or 'unassigned'. Ignored for a plain admin."
// @Param        kind       query     string  false  "Instrument filter"  Enums(life, health, motor, deposit)
// @Param        window     query     string  false  "How far ahead to look. A day window also includes everything overdue."  Enums(overdue, 7, 30, 90, all)
// @Param        page       query     int     false  "Page number (default 1)"
// @Param        limit      query     int     false  "Rows per page (default 50, max 200)"
// @Success      200  {object}  response.APIResponse{data=domain.RenewalPage}  "Renewals retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — agency staff only"
// @Failure      500  {object}  response.APIResponse  "Internal server error"
// @Security     BearerAuth
// @Router       /renewals [get]
func (h *RenewalHandler) GetRenewals(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "50"), 10, 64)

	result, err := h.renewalService.GetRenewals(c.Request.Context(), claims.Role, claims.UserID.Hex(), domain.RenewalQuery{
		AgencyID: c.Query("agency_id"),
		Kind:     c.Query("kind"),
		Window:   c.Query("window"),
		Search:   c.Query("q"),
		Page:     page,
		Limit:    limit,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Renewals retrieved successfully", result)
}
