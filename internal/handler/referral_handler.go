package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// ReferralHandler handles HTTP requests for referral scheme operations.
type ReferralHandler struct {
	referralService domain.ReferralService
}

// NewReferralHandler creates a new ReferralHandler.
func NewReferralHandler(referralService domain.ReferralService) *ReferralHandler {
	return &ReferralHandler{
		referralService: referralService,
	}
}

// GetMyStats retrieves referral statistics, referral code, and app validity end date for the authenticated client.
// @Summary      Get client referral stats and code
// @Description  Returns the logged-in user's unique referral code, current app validity end date, pending referrals, completed referrals, and total extra days earned.
// @Tags         Referrals
// @Produce      json
// @Success      200  {object}  response.APIResponse{data=domain.ReferralStatsDTO}  "Referral stats retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Failure      500  {object}  response.APIResponse  "Internal server error"
// @Security     BearerAuth
// @Router       /referrals/my-stats [get]
func (h *ReferralHandler) GetMyStats(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}

	stats, err := h.referralService.GetMyStats(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Referral stats retrieved successfully", stats)
}

// GetAllReferrals retrieves a paginated master list of all referral records across the agency (Admin only).
// @Summary      Get all agency referral records (Admin only)
// @Description  Retrieves a paginated master list of all referrals across the platform with referrer names, emails, statuses, and credited reward days.
// @Tags         Referrals
// @Produce      json
// @Param        page   query     int  false  "Page number (default: 1)"
// @Param        limit  query     int  false  "Items per page (default: 10, max: 100)"
// @Success      200    {object}  response.APIResponse{data=domain.ReferralListResponse}  "Referral records retrieved successfully"
// @Failure      401    {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Failure      403    {object}  response.APIResponse  "Forbidden — admin role required"
// @Failure      500    {object}  response.APIResponse  "Internal server error"
// @Security     BearerAuth
// @Router       /referrals/all [get]
func (h *ReferralHandler) GetAllReferrals(c *gin.Context) {
	page, _ := strconv.ParseInt(c.Query("page"), 10, 64)
	limit, _ := strconv.ParseInt(c.Query("limit"), 10, 64)

	res, err := h.referralService.GetAllReferrals(c.Request.Context(), page, limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Referral records retrieved successfully", res)
}
