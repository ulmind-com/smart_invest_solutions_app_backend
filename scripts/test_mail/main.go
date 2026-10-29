// scripts/test_mail.go
// Run: go run scripts/test_mail.go
// Purpose: Test Resend email delivery using .env config

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Html    string   `json:"html"`
}

func main() {
	// Load .env from project root
	if err := godotenv.Load(".env"); err != nil {
		log.Fatalf("❌ .env file load korte parini: %v", err)
	}

	apiKey := os.Getenv("RESEND_API_KEY")
	fromAddress := os.Getenv("MAIL_ADDRESS")
	toEmail := "samiran.samanta.dev@gmail.com"

	if apiKey == "" {
		log.Fatal("❌ RESEND_API_KEY .env te pawa gelo na!")
	}
	if fromAddress == "" {
		log.Fatal("❌ MAIL_ADDRESS .env te pawa gelo na!")
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📧 Smart Invest Solutions — Resend Test")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("   From    : %s\n", fromAddress)
	fmt.Printf("   To      : %s\n", toEmail)
	fmt.Printf("   API Key : %s...%s\n", apiKey[:6], apiKey[len(apiKey)-4:])
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	htmlBody := `
<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <style>
    body { font-family: 'Helvetica Neue', Arial, sans-serif; background: #f4f6f9; margin: 0; padding: 40px 20px; }
    .card { background: #ffffff; border-radius: 12px; max-width: 520px; margin: 0 auto;
            padding: 40px; box-shadow: 0 4px 20px rgba(0,0,0,0.08); }
    .badge { display: inline-block; background: #16a34a; color: #fff; font-size: 12px;
             font-weight: 600; padding: 4px 12px; border-radius: 999px; margin-bottom: 24px; }
    h1 { color: #1a1a2e; font-size: 24px; margin: 0 0 16px; }
    p { color: #4a5568; font-size: 15px; line-height: 1.7; margin: 0 0 12px; }
    .info-box { background: #f0fdf4; border: 1px solid #bbf7d0; border-radius: 8px;
                padding: 16px 20px; margin: 20px 0; }
    .info-box p { color: #166534; font-size: 14px; margin: 4px 0; }
    .footer { text-align: center; margin-top: 32px; color: #9ca3af; font-size: 12px; }
  </style>
</head>
<body>
  <div class="card">
    <div class="badge">✅ Test Successful</div>
    <h1>Resend Mail Delivery Confirmed</h1>
    <p>Hey Samiran! তোমার <strong>Smart Invest Solutions</strong> backend-এর Resend email configuration সঠিকভাবে কাজ করছে।</p>
    <div class="info-box">
      <p>🔑 <strong>API Key:</strong> Valid &amp; Authenticated</p>
      <p>📤 <strong>Sender:</strong> noreply@smartinvestsolutions.in</p>
      <p>📥 <strong>Receiver:</strong> samiran.samanta.dev@gmail.com</p>
      <p>🚀 <strong>Status:</strong> Delivered Successfully</p>
    </div>
    <p>এই mail টা পেলে বুঝবে যে production-এ mail service properly configured আছে এবং users-রা সব transactional email পাবে।</p>
    <div class="footer">Smart Invest Solutions &bull; Automated Test Mail &bull; Do not reply</div>
  </div>
</body>
</html>`

	payload := resendRequest{
		From:    fmt.Sprintf("Smart Invest Solutions <%s>", fromAddress),
		To:      []string{toEmail},
		Subject: "✅ Resend Test — Smart Invest Solutions",
		Html:    htmlBody,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("❌ JSON marshal error: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewBuffer(jsonBytes))
	if err != nil {
		log.Fatalf("❌ Request তৈরি করতে পারিনি: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	fmt.Println("\n⏳ Resend API-তে request পাঠাচ্ছি...")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("❌ HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := strings.TrimSpace(string(body))

	fmt.Printf("\n📡 HTTP Status: %d\n", resp.StatusCode)
	fmt.Printf("📄 Response   : %s\n", bodyStr)

	if resp.StatusCode == 200 || resp.StatusCode == 201 {
		fmt.Println("\n🎉 SUCCESS! Mail সফলভাবে পাঠানো হয়েছে।")
		fmt.Printf("   👉 Check koro: %s\n", toEmail)
	} else {
		fmt.Println("\n❌ FAILED! Mail পাঠানো যায়নি।")
		fmt.Println("   Resend dashboard চেক করো: https://resend.com/emails")
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}
