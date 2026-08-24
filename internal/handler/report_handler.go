package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// ReportHandler handles HTTP requests for downloadable client reports.
type ReportHandler struct {
	service domain.ReportService
}

// NewReportHandler creates a new instance of ReportHandler.
func NewReportHandler(service domain.ReportService) *ReportHandler {
	return &ReportHandler{service: service}
}

// GetClientPortfolio handles generating and downloading a client's portfolio PDF.
// @Summary      Download Client Portfolio Report
// @Description  Generates a PDF summarizing the client's details, family members, and Life/Health/General Insurance + Fixed Deposit holdings. Clients always receive their own report; admin/super_admin may generate one for any client via the user_id query parameter.
// @Tags         Reports
// @Accept       json
// @Produce      application/pdf
// @Param        user_id  query  string  false  "Target client's User ID — Admin/Super Admin only"
// @Success      200  {file}    file  "Portfolio PDF"
// @Failure      400  {object}  response.APIResponse  "Bad request"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /reports/portfolio [get]
func (h *ReportHandler) GetClientPortfolio(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	targetUserID := claims.UserID.Hex()
	if claims.Role == domain.RoleAdmin || claims.Role == domain.RoleSuperAdmin {
		if requested := c.Query("user_id"); requested != "" {
			targetUserID = requested
		}
	}

	pdfBytes, err := h.service.GenerateClientPortfolio(c.Request.Context(), targetUserID)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	c.Header("Content-Disposition", `attachment; filename="portfolio_report.pdf"`)
	c.Data(http.StatusOK, "application/pdf", pdfBytes)
}
