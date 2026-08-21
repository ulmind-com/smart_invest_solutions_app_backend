package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/smart-invest-solutions/backend/internal/config"
)

// EmailService defines the interface for sending emails.
type EmailService interface {
	SendWelcomeEmail(ctx context.Context, toEmail, name string) error
	SendCredentialsEmail(ctx context.Context, toEmail, name, password string) error
	SendRejectionEmail(ctx context.Context, toEmail, name, reason string) error
	SendOTPEmail(ctx context.Context, toEmail, otpCode string) error
	SendPasswordResetConfirmationEmail(ctx context.Context, toEmail string) error
	SendAccountDeletionEmail(ctx context.Context, toEmail, name string) error
}

// ResendService implements EmailService using the Resend HTTP API.
type ResendService struct {
	apiKey      string
	fromAddress string
	httpClient  *http.Client
}

// NewResendService initializes a new Resend email service.
func NewResendService(cfg *config.Config) *ResendService {
	return &ResendService{
		apiKey:      cfg.ResendAPIKey,
		fromAddress: cfg.MailAddress,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type resendSendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

type resendResponse struct {
	ID    string `json:"id"`
	Error *struct {
		Message string `json:"message"`
		Name    string `json:"name"`
	} `json:"error,omitempty"`
}

// SendWelcomeEmail sends an automatic welcome email upon successful user registration.
func (s *ResendService) SendWelcomeEmail(ctx context.Context, toEmail, name string) error {
	subject := "🎉 Welcome to Smart Invest Solutions! Account Under Verification"

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <style>
        body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background-color: #0f172a; margin: 0; padding: 20px; color: #f8fafc; }
        .container { max-width: 550px; margin: 0 auto; background: #1e293b; border-radius: 16px; padding: 30px; border: 1px solid #334155; }
        .header { text-align: center; margin-bottom: 24px; }
        .header h1 { color: #38bdf8; font-size: 24px; margin: 0; }
        .welcome-box { background: #0f172a; border-left: 4px solid #38bdf8; border-radius: 8px; padding: 20px; margin: 20px 0; }
        .status-badge { display: inline-block; background: #f59e0b; color: #0f172a; font-weight: bold; font-size: 12px; padding: 4px 12px; border-radius: 12px; text-transform: uppercase; }
        .footer { text-align: center; margin-top: 24px; font-size: 12px; color: #64748b; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Smart Invest Solutions</h1>
            <p style="color: #94a3b8; font-size: 14px; margin-top: 4px;">LIC Policy & Investment Management</p>
        </div>
        
        <h2>Welcome, %s! 👋</h2>
        <p>Thank you for creating an account with <strong>Smart Invest Solutions</strong>.</p>
        
        <div class="welcome-box">
            <p style="margin: 0 0 10px 0;">Account Status: <span class="status-badge">⏳ Pending Admin Verification</span></p>
            <p style="font-size: 13px; color: #cbd5e1; margin: 0;">
                Your account registration details have been submitted to our Admin team. Once verified, you will receive an approval email allowing full access to your Dashboard, E-Vault, Family Details, and Insurance policies.
            </p>
        </div>

        <p style="font-size: 13px; color: #94a3b8;">If you have any urgent queries, feel free to reply directly to this email.</p>
        
        <div class="footer">&copy; Smart Invest Solutions. All rights reserved.</div>
    </div>
</body>
</html>
`, name)

	return s.sendResendRequest(ctx, subject, toEmail, htmlBody)
}

// SendCredentialsEmail sends an email containing login confirmation/credentials to an approved client.
func (s *ResendService) SendCredentialsEmail(ctx context.Context, toEmail, name, password string) error {
	subject := "🎉 Your Access Has Been Approved — Smart Invest Solutions"

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <style>
        body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background-color: #f4f7f6; margin: 0; padding: 20px; color: #333; }
        .container { max-width: 600px; margin: 0 auto; background: #ffffff; border-radius: 12px; overflow: hidden; box-shadow: 0 4px 15px rgba(0,0,0,0.08); }
        .header { background: linear-gradient(135deg, #1e3c72 0%%, #2a5298 100%%); color: #ffffff; padding: 30px 20px; text-align: center; }
        .header h1 { margin: 0; font-size: 24px; letter-spacing: 0.5px; }
        .header p { margin: 5px 0 0 0; opacity: 0.9; font-size: 14px; }
        .content { padding: 30px 25px; line-height: 1.6; }
        .credentials-box { background: #f8fafc; border-left: 4px solid #2a5298; border-radius: 6px; padding: 20px; margin: 25px 0; }
        .field { margin-bottom: 12px; }
        .field-label { font-size: 12px; text-transform: uppercase; color: #64748b; font-weight: bold; letter-spacing: 0.5px; }
        .field-value { font-size: 18px; color: #0f172a; font-family: monospace; font-weight: bold; }
        .footer { background: #f1f5f9; padding: 15px; text-align: center; font-size: 12px; color: #64748b; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Smart Invest Solutions</h1>
            <p>LIC Policy & Investment Management</p>
        </div>
        <div class="content">
            <h2>Hello %s,</h2>
            <p>Great news! Your access request to <strong>Smart Invest Solutions</strong> has been approved by the Admin.</p>
            <p>You can now log in to your account using your registered credentials:</p>

            <div class="credentials-box">
                <div class="field">
                    <div class="field-label">User ID / Email</div>
                    <div class="field-value">%s</div>
                </div>
                <div class="field" style="margin-bottom: 0;">
                    <div class="field-label">Password Status</div>
                    <div class="field-value">%s</div>
                </div>
            </div>

            <p>Open the <strong>Smart Invest Solutions App</strong> to access your dashboard, E-Vault, family details, and policies.</p>
        </div>
        <div class="footer">
            &copy; Smart Invest Solutions. All rights reserved.
        </div>
    </div>
</body>
</html>
`, name, toEmail, password)

	return s.sendResendRequest(ctx, subject, toEmail, htmlBody)
}

// SendRejectionEmail sends a notification if an access request or account status is set to inactive.
func (s *ResendService) SendRejectionEmail(ctx context.Context, toEmail, name, reason string) error {
	subject := "Update on Your Access Request — Smart Invest Solutions"

	if reason == "" {
		reason = "We are unable to verify your submitted details at this time."
	}

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<body style="font-family: sans-serif; padding: 20px; color: #333;">
    <h2>Hello %s,</h2>
    <p>Thank you for your interest in Smart Invest Solutions.</p>
    <p>Regrettably, your account status update: <strong>%s</strong></p>
    <p>If you believe this is in error, please feel free to contact support.</p>
    <br>
    <p>Best regards,<br>Smart Invest Solutions Team</p>
</body>
</html>
`, name, reason)

	return s.sendResendRequest(ctx, subject, toEmail, htmlBody)
}

// SendOTPEmail sends a 6-digit OTP code for password reset.
func (s *ResendService) SendOTPEmail(ctx context.Context, toEmail, otpCode string) error {
	subject := "🔑 Your Password Reset OTP — Smart Invest Solutions"

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <style>
        body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background-color: #0f172a; margin: 0; padding: 20px; color: #f8fafc; }
        .container { max-width: 500px; margin: 0 auto; background: #1e293b; border-radius: 16px; padding: 30px; border: 1px solid #334155; }
        .header { text-align: center; margin-bottom: 24px; }
        .header h1 { color: #38bdf8; font-size: 22px; margin: 0; }
        .otp-box { background: #0f172a; border: 2px dashed #38bdf8; border-radius: 12px; padding: 20px; text-align: center; margin: 24px 0; }
        .otp-code { font-size: 36px; font-weight: bold; font-family: monospace; letter-spacing: 8px; color: #34d399; }
        .info { color: #94a3b8; font-size: 14px; line-height: 1.5; text-align: center; }
        .footer { text-align: center; margin-top: 24px; font-size: 12px; color: #64748b; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Smart Invest Solutions</h1>
            <p style="color: #94a3b8; font-size: 14px; margin-top: 4px;">Password Reset Request</p>
        </div>
        <p class="info">You requested to reset your password. Use the verification OTP code below to proceed:</p>
        
        <div class="otp-box">
            <div class="otp-code">%s</div>
        </div>

        <p class="info">⏰ This code is valid for <strong>10 minutes</strong>. Do not share this code with anyone.</p>
        <div class="footer">&copy; Smart Invest Solutions. If you did not request this, please ignore.</div>
    </div>
</body>
</html>
`, otpCode)

	return s.sendResendRequest(ctx, subject, toEmail, htmlBody)
}

// SendPasswordResetConfirmationEmail sends a security alert email after successful password reset.
func (s *ResendService) SendPasswordResetConfirmationEmail(ctx context.Context, toEmail string) error {
	subject := "✅ Password Successfully Reset — Smart Invest Solutions"

	htmlBody := `
<!DOCTYPE html>
<html>
<body style="font-family: sans-serif; padding: 20px; background-color: #0f172a; color: #f8fafc;">
    <div style="max-width: 500px; margin: 0 auto; background: #1e293b; padding: 24px; border-radius: 12px;">
        <h2 style="color: #34d399;">Password Changed Successfully</h2>
        <p>Your password for Smart Invest Solutions has been successfully updated.</p>
        <p style="color: #94a3b8; font-size: 13px;">If you performed this action, no further steps are needed.</p>
        <p style="color: #ef4444; font-size: 13px;">If you did NOT request this change, please contact support immediately.</p>
    </div>
</body>
</html>
`

	return s.sendResendRequest(ctx, subject, toEmail, htmlBody)
}

// SendAccountDeletionEmail sends a confirmation email notifying the user of permanent account & data erasure.
func (s *ResendService) SendAccountDeletionEmail(ctx context.Context, toEmail, name string) error {
	subject := "⚠️ Account & Data Permanently Deleted — Smart Invest Solutions"

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<body style="font-family: sans-serif; padding: 20px; background-color: #0f172a; color: #f8fafc;">
    <div style="max-width: 500px; margin: 0 auto; background: #1e293b; padding: 24px; border-radius: 12px; border: 1px solid #ef4444;">
        <h2 style="color: #ef4444;">Account Permanently Deleted</h2>
        <p>Hello %s,</p>
        <p>Your account with <strong>Smart Invest Solutions</strong> and all associated data (including E-Vault documents, family details, and insurance policies) have been permanently erased from our system.</p>
        <p style="color: #94a3b8; font-size: 13px;">Thank you for using Smart Invest Solutions. If you did not request this account deletion, please contact support immediately.</p>
    </div>
</body>
</html>
`, name)

	return s.sendResendRequest(ctx, subject, toEmail, htmlBody)
}

// sendResendRequest executes the HTTP POST to Resend API with logging and automatic onboarding@resend.dev fallback.
func (s *ResendService) sendResendRequest(ctx context.Context, subject, toEmail, htmlContent string) error {
	if s.apiKey == "" {
		log.Printf("[Resend Error] RESEND_API_KEY is missing in environment")
		return fmt.Errorf("RESEND_API_KEY is not configured")
	}

	fromAddress := s.fromAddress
	if fromAddress == "" {
		fromAddress = "onboarding@resend.dev"
	}

	fromHeader := fmt.Sprintf("Smart Invest Solutions <%s>", fromAddress)

	reqBody := resendSendRequest{
		From:    fromHeader,
		To:      []string{toEmail},
		Subject: subject,
		HTML:    htmlContent,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	err = s.executePost(ctx, jsonBytes)
	if err != nil && fromAddress != "onboarding@resend.dev" {
		log.Printf("[Resend Fallback] Primary sender %s failed (%v). Retrying with onboarding@resend.dev...", fromAddress, err)
		reqBody.From = "Smart Invest Solutions <onboarding@resend.dev>"
		fallbackBytes, _ := json.Marshal(reqBody)
		err = s.executePost(ctx, fallbackBytes)
	}

	return err
}

func (s *ResendService) executePost(ctx context.Context, jsonBytes []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("[Resend HTTP Error] %v", err)
		return err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[Resend API Error %d] %s", resp.StatusCode, string(bodyBytes))
		return fmt.Errorf("resend status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	log.Printf("[Resend Success %d] Email delivered successfully", resp.StatusCode)
	return nil
}
