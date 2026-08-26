package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// EmailVerificationHandler handles HTTP requests for signup email OTP verification.
type EmailVerificationHandler struct {
	verifService domain.EmailVerificationService
}

// NewEmailVerificationHandler creates a new EmailVerificationHandler.
func NewEmailVerificationHandler(verifService domain.EmailVerificationService) *EmailVerificationHandler {
	return &EmailVerificationHandler{
		verifService: verifService,
	}
}

// VerifyEmailOTP verifies the 6-digit OTP sent to the user's email during signup.
// @Summary      Verify signup email OTP
// @Description  Validates the 6-digit OTP code sent to the user's email upon registration. Once verified, the account is marked email-verified and submitted to Admin for approval.
// @Tags         User Authentication
// @Accept       json
// @Produce      json
// @Param        request  body      domain.VerifyEmailOTPRequest  true  "OTP Verification Payload"
// @Success      200      {object}  response.APIResponse          "Email verified successfully"
// @Failure      400      {object}  response.APIResponse          "Invalid or expired OTP code"
// @Failure      500      {object}  response.APIResponse          "Internal server error"
// @Router       /users/verify-email-otp [post]
func (h *EmailVerificationHandler) VerifyEmailOTP(c *gin.Context) {
	var req domain.VerifyEmailOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request payload: "+err.Error())
		return
	}

	if err := h.verifService.VerifyOTP(c.Request.Context(), &req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "Email verified successfully! Your account registration details have been submitted to Admin for approval.", nil)
}

// ResendEmailOTP requests a fresh 6-digit OTP code to be sent to the user's email inbox.
// @Summary      Resend signup email OTP
// @Description  Generates and dispatches a fresh 6-digit OTP code to the user's email address (subject to a 60-second rate limit cooldown).
// @Tags         User Authentication
// @Accept       json
// @Produce      json
// @Param        request  body      domain.ResendEmailOTPRequest  true  "Resend OTP Payload"
// @Success      200      {object}  response.APIResponse          "Verification OTP sent successfully"
// @Failure      400      {object}  response.APIResponse          "Rate limit exceeded or email already verified"
// @Failure      500      {object}  response.APIResponse          "Internal server error"
// @Router       /users/resend-email-otp [post]
func (h *EmailVerificationHandler) ResendEmailOTP(c *gin.Context) {
	var req domain.ResendEmailOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request payload: "+err.Error())
		return
	}

	if err := h.verifService.ResendOTP(c.Request.Context(), &req); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, "A fresh verification OTP code has been sent to your email inbox.", nil)
}
