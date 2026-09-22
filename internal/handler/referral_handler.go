package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// ReferralHandler handles HTTP requests for the staff referral scheme.
type ReferralHandler struct {
	referralService domain.ReferralService
}

// NewReferralHandler creates a new ReferralHandler.
func NewReferralHandler(referralService domain.ReferralService) *ReferralHandler {
	return &ReferralHandler{
		referralService: referralService,
	}
}

// GetMyStats returns the calling staff member's referral code and their referral counts.
// @Summary      Get my referral code and stats
// @Description  Returns the logged-in admin's referral code, plus how many referred clients are pending approval and how many have joined. Agency staff only — clients have no referral code.
// @Tags         Referrals
// @Produce      json
// @Success      200  {object}  response.APIResponse{data=domain.ReferralStatsDTO}  "Referral stats retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Failure      403  {object}  response.APIResponse  "Forbidden — agency staff only"
// @Security     BearerAuth
// @Router       /referrals/my-stats [get]
func (h *ReferralHandler) GetMyStats(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	stats, err := h.referralService.GetMyStats(c.Request.Context(), claims.Role, claims.UserID.Hex())
	if err != nil {
		response.Error(c, http.StatusForbidden, err.Error())
		return
	}

	response.Success(c, "Referral stats retrieved successfully", stats)
}

// GetAllReferrals lists the referral ledger.
// @Summary      List referrals
// @Description  A plain admin sees only the clients they referred themselves. A super admin sees every referral, and can narrow the list to one admin with ?referrer_id=.
// @Tags         Referrals
// @Produce      json
// @Param        referrer_id  query     string  false  "Super admin only — list a single admin's referrals"
// @Param        page         query     int     false  "Page number (default 1)"
// @Param        limit        query     int     false  "Rows per page (default 20, max 100)"
// @Success      200  {object}  response.APIResponse{data=domain.ReferralListResponse}  "Referral records retrieved successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request — invalid referrer ID"
// @Failure      401  {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Security     BearerAuth
// @Router       /referrals/all [get]
func (h *ReferralHandler) GetAllReferrals(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "20"), 10, 64)

	res, err := h.referralService.GetAllReferrals(c.Request.Context(), claims.Role, claims.UserID.Hex(), c.Query("referrer_id"), page, limit)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Referral records retrieved successfully", res)
}

// GetAdminSummary returns the per-admin referral leaderboard (Super Admin only).
// @Summary      Per-admin referral summary
// @Description  Every staff account with the number of clients they referred — pending, joined and total — highest conversions first.
// @Tags         Referrals
// @Produce      json
// @Success      200  {object}  response.APIResponse{data=[]domain.AdminReferralSummary}  "Referral summary retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Failure      403  {object}  response.APIResponse  "Forbidden — super admin only"
// @Security     BearerAuth
// @Router       /referrals/summary [get]
func (h *ReferralHandler) GetAdminSummary(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	summary, err := h.referralService.GetAdminSummary(c.Request.Context(), claims.Role)
	if err != nil {
		response.Error(c, http.StatusForbidden, err.Error())
		return
	}

	response.Success(c, "Referral summary retrieved successfully", summary)
}
