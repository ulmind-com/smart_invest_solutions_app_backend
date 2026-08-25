package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// CalculatorHandler handles HTTP requests for financial calculators and settings management.
type CalculatorHandler struct {
	calculatorService domain.CalculatorService
}

// NewCalculatorHandler creates a new CalculatorHandler.
func NewCalculatorHandler(calculatorService domain.CalculatorService) *CalculatorHandler {
	return &CalculatorHandler{
		calculatorService: calculatorService,
	}
}

// GetSettings retrieves the global default return rates for financial calculators.
// @Summary      Get global calculator default rates
// @Description  Retrieves the global default SIP, Lumpsum, and FD return rates configured by Admin.
// @Tags         Calculators
// @Produce      json
// @Success      200  {object}  response.APIResponse{data=domain.CalculatorSettings}  "Calculator settings retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Failure      500  {object}  response.APIResponse  "Internal server error"
// @Security     BearerAuth
// @Router       /calculators/settings [get]
func (h *CalculatorHandler) GetSettings(c *gin.Context) {
	settings, err := h.calculatorService.GetSettings(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Calculator settings retrieved successfully", settings)
}

// UpdateSettings updates the global default return rates for financial calculators (Admin/Super Admin only).
// @Summary      Update global calculator default rates (Admin only)
// @Description  Updates the global default SIP, Lumpsum, or FD return rates used across client calculators when no rate is explicitly passed.
// @Tags         Calculators
// @Accept       json
// @Produce      json
// @Param        payload  body      domain.UpdateCalculatorSettingsDTO  true  "Updated calculator default rates"
// @Success      200      {object}  response.APIResponse{data=domain.CalculatorSettings}  "Calculator settings updated successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request — invalid input format"
// @Failure      401      {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Failure      403      {object}  response.APIResponse  "Forbidden — admin role required"
// @Failure      500      {object}  response.APIResponse  "Internal server error"
// @Security     BearerAuth
// @Router       /calculators/settings [put]
func (h *CalculatorHandler) UpdateSettings(c *gin.Context) {
	var dto domain.UpdateCalculatorSettingsDTO
	if err := c.ShouldBindJSON(&dto); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request payload: "+err.Error())
		return
	}

	settings, err := h.calculatorService.UpdateSettings(c.Request.Context(), &dto)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Calculator settings updated successfully", settings)
}

// CalculateSIP calculates Systematic Investment Plan (SIP) returns.
// @Summary      Calculate SIP returns
// @Description  Calculates total invested amount, estimated returns, and final maturity value for a Systematic Investment Plan (SIP). If expected_return_rate is omitted, the Admin's default SIP rate is applied.
// @Tags         Calculators
// @Accept       json
// @Produce      json
// @Param        payload  body      domain.SIPRequestDTO  true  "SIP calculation parameters"
// @Success      200      {object}  response.APIResponse{data=domain.CalculatorResponseDTO}  "SIP calculation completed successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request — invalid parameters"
// @Failure      401      {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Failure      500      {object}  response.APIResponse  "Internal server error"
// @Security     BearerAuth
// @Router       /calculators/sip [post]
func (h *CalculatorHandler) CalculateSIP(c *gin.Context) {
	var req domain.SIPRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid SIP request parameters: "+err.Error())
		return
	}

	result, err := h.calculatorService.CalculateSIP(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "SIP calculation completed successfully", result)
}

// CalculateLumpsum calculates single investment compound interest returns.
// @Summary      Calculate Lumpsum returns
// @Description  Calculates total invested amount, estimated returns, and final maturity value for a Lumpsum investment. If expected_return_rate is omitted, the Admin's default Lumpsum rate is applied.
// @Tags         Calculators
// @Accept       json
// @Produce      json
// @Param        payload  body      domain.LumpsumRequestDTO  true  "Lumpsum calculation parameters"
// @Success      200      {object}  response.APIResponse{data=domain.CalculatorResponseDTO}  "Lumpsum calculation completed successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request — invalid parameters"
// @Failure      401      {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Failure      500      {object}  response.APIResponse  "Internal server error"
// @Security     BearerAuth
// @Router       /calculators/lumpsum [post]
func (h *CalculatorHandler) CalculateLumpsum(c *gin.Context) {
	var req domain.LumpsumRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid Lumpsum request parameters: "+err.Error())
		return
	}

	result, err := h.calculatorService.CalculateLumpsum(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Lumpsum calculation completed successfully", result)
}

// CalculateFD calculates Fixed Deposit maturity returns based on compounding frequency.
// @Summary      Calculate Fixed Deposit (FD) returns
// @Description  Calculates total principal, estimated interest returns, and maturity value for a Fixed Deposit (FD). If interest_rate is omitted, the Admin's default FD rate is applied.
// @Tags         Calculators
// @Accept       json
// @Produce      json
// @Param        payload  body      domain.FDRequestDTO  true  "FD calculation parameters"
// @Success      200      {object}  response.APIResponse{data=domain.CalculatorResponseDTO}  "FD calculation completed successfully"
// @Failure      400      {object}  response.APIResponse  "Bad request — invalid parameters"
// @Failure      401      {object}  response.APIResponse  "Unauthorized — token missing or invalid"
// @Failure      500      {object}  response.APIResponse  "Internal server error"
// @Security     BearerAuth
// @Router       /calculators/fd [post]
func (h *CalculatorHandler) CalculateFD(c *gin.Context) {
	var req domain.FDRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid FD request parameters: "+err.Error())
		return
	}

	result, err := h.calculatorService.CalculateFD(c.Request.Context(), &req)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "FD calculation completed successfully", result)
}
