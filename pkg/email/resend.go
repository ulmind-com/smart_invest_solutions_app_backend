package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/smart-invest-solutions/backend/internal/config"
)

// EmailService defines the interface for sending emails.
type EmailService interface {
	SendCredentialsEmail(ctx context.Context, toEmail, name, password string) error
	SendRejectionEmail(ctx context.Context, toEmail, name, reason string) error
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

// SendCredentialsEmail sends an email containing generated login credentials to an approved client.
func (s *ResendService) SendCredentialsEmail(ctx context.Context, toEmail, name, password string) error {
	if s.apiKey == "" {
		return fmt.Errorf("RESEND_API_KEY is not configured")
	}

	subject := "🎉 Your Access Has Been Approved — Smart Invest Solutions"
	fromHeader := fmt.Sprintf("Smart Invest Solutions <%s>", s.fromAddress)

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
        .btn { display: inline-block; background: #2a5298; color: #ffffff; text-decoration: none; padding: 12px 25px; border-radius: 6px; font-weight: bold; margin-top: 15px; }
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
            <p>You can now log in to your account using the generated credentials below:</p>

            <div class="credentials-box">
                <div class="field">
                    <div class="field-label">User ID / Email</div>
                    <div class="field-value">%s</div>
                </div>
                <div class="field" style="margin-bottom: 0;">
                    <div class="field-label">Temporary Password</div>
                    <div class="field-value">%s</div>
                </div>
            </div>

            <p style="font-size: 13px; color: #ef4444;">⚠️ <em>For security reasons, please change your password after your initial login.</em></p>

            <p>Open the <strong>Smart Invest Solutions App</strong> to access your dashboard, policies, and investment insights.</p>
        </div>
        <div class="footer">
            &copy; Smart Invest Solutions. All rights reserved.<br>
            Sent via %s
        </div>
    </div>
</body>
</html>
`, name, toEmail, password, s.fromAddress)

	reqBody := resendSendRequest{
		From:    fromHeader,
		To:      []string{toEmail},
		Subject: subject,
		HTML:    htmlBody,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal email request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute http request to Resend: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("resend API returned error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var resResp resendResponse
	if err := json.Unmarshal(bodyBytes, &resResp); err == nil && resResp.Error != nil {
		return fmt.Errorf("resend API error: %s", resResp.Error.Message)
	}

	return nil
}

// SendRejectionEmail sends a notification if an access request is rejected.
func (s *ResendService) SendRejectionEmail(ctx context.Context, toEmail, name, reason string) error {
	if s.apiKey == "" {
		return nil // Non-fatal if key not set
	}

	subject := "Update on Your Access Request — Smart Invest Solutions"
	fromHeader := fmt.Sprintf("Smart Invest Solutions <%s>", s.fromAddress)

	if reason == "" {
		reason = "We are unable to verify your submitted details at this time."
	}

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<body style="font-family: sans-serif; padding: 20px; color: #333;">
    <h2>Hello %s,</h2>
    <p>Thank you for your interest in Smart Invest Solutions.</p>
    <p>Regrettably, your request for access could not be approved at this time.</p>
    <p><strong>Reason:</strong> %s</p>
    <p>If you believe this is in error, please feel free to submit a new request with updated information.</p>
    <br>
    <p>Best regards,<br>Smart Invest Solutions Team</p>
</body>
</html>
`, name, reason)

	reqBody := resendSendRequest{
		From:    fromHeader,
		To:      []string{toEmail},
		Subject: subject,
		HTML:    htmlBody,
	}

	jsonBytes, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
