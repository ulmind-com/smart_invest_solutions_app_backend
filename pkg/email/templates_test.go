package email

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRenderPolicyCreatedEmail(t *testing.T) {
	data := PolicyNotificationData{
		PolicyType:         "Life Insurance",
		PolicyNo:           "412345678",
		PlanName:           "Jeevan Labh",
		CompanyName:        "LIC of India",
		InsuredName:        "Akash Jana",
		NomineeName:        "Pooja Jana",
		SumAssured:         500000,
		InstallmentPremium: 18450,
		PaymentMode:        "Yearly",
		NextDueDate:        time.Date(2027, time.March, 15, 0, 0, 0, 0, time.UTC),
		DOC:                time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC),
		AdvisorName:        "Bikash Sharma",
		AdvisorContact:     "+91 9876543210",
	}

	html := RenderPolicyCreatedEmail("Akash Jana", data)

	assert.Contains(t, html, "SmartWealth")
	assert.Contains(t, html, "SMART INVEST SOLUTIONS")
	assert.Contains(t, html, "Akash Jana")
	assert.Contains(t, html, "412345678")
	assert.Contains(t, html, "Jeevan Labh")
	assert.Contains(t, html, "₹18450.00")
	assert.Contains(t, html, "₹500000.00")
	assert.Contains(t, html, "15 Mar 2027")
	assert.Contains(t, html, "Bikash Sharma")
	assert.Contains(t, html, "+91 9876543210")
	assert.Contains(t, html, "Security Advisory:")
	assert.Contains(t, html, "View Policy in SmartWealth")
}

func TestRenderPolicyUpdatedEmail(t *testing.T) {
	data := PolicyNotificationData{
		PolicyType:         "Life Insurance",
		PolicyNo:           "987654321",
		PlanName:           "Jeevan Umang",
		CompanyName:        "LIC of India",
		InsuredName:        "Ramesh Ghosh",
		InstallmentPremium: 25000,
		PaymentMode:        "Half-Yearly",
		NextDueDate:        time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
		AdvisorName:        "Asha Roy",
	}

	html := RenderPolicyUpdatedEmail("Ramesh Ghosh", data)

	assert.Contains(t, html, "Policy Details Updated")
	assert.Contains(t, html, "987654321")
	assert.Contains(t, html, "Ramesh Ghosh")
	assert.Contains(t, html, "Jeevan Umang")
	assert.Contains(t, html, "₹25000.00")
	assert.Contains(t, html, "Asha Roy")
}

func TestRenderPaymentReceiptEmail(t *testing.T) {
	data := PaymentReceiptData{
		PolicyType:     "Life Insurance",
		PolicyNo:       "112233445",
		PlanName:       "New Endowment",
		CompanyName:    "LIC of India",
		InsuredName:    "Mitali Sen",
		AmountPaid:     12500,
		PaidAt:         time.Date(2026, time.November, 21, 0, 0, 0, 0, time.UTC),
		NextDueDate:    time.Date(2027, time.November, 21, 0, 0, 0, 0, time.UTC),
		PaymentMode:    "Yearly",
		AdvisorName:    "Agency Admin",
		AdvisorContact: "agency@smartinvest.com",
	}

	html := RenderPaymentReceiptEmail("Mitali Sen", data)

	assert.Contains(t, html, "Premium Payment Acknowledged")
	assert.Contains(t, html, "112233445")
	assert.Contains(t, html, "₹12500.00")
	assert.Contains(t, html, "21 Nov 2026")
	assert.Contains(t, html, "21 Nov 2027")
	assert.Contains(t, html, "Open SmartWealth E-Vault")
}

func TestRenderRenewalReminderEmail(t *testing.T) {
	// 1. Upcoming renewal
	dueData := RenewalReminderData{
		PolicyType:     "Life Insurance",
		PolicyNo:       "556677889",
		PlanName:       "Bima Jyoti",
		CompanyName:    "LIC of India",
		InsuredName:    "Sourav Das",
		AmountDue:      8400,
		DueDate:        time.Date(2026, time.December, 5, 0, 0, 0, 0, time.UTC),
		DaysLeft:       14,
		IsOverdue:      false,
		AdvisorName:    "Pradip Saha",
		AdvisorContact: "9830000000",
	}

	dueHTML := RenderRenewalReminderEmail("Sourav Das", dueData)
	assert.Contains(t, dueHTML, "Premium Due Soon")
	assert.Contains(t, dueHTML, "Due in 14 Days")
	assert.Contains(t, dueHTML, "₹8400.00")
	assert.Contains(t, dueHTML, "05 Dec 2026")
	assert.Contains(t, dueHTML, "Grace Period Information:")

	// 2. Overdue renewal
	overdueData := RenewalReminderData{
		PolicyType:     "Life Insurance",
		PolicyNo:       "556677889",
		PlanName:       "Bima Jyoti",
		CompanyName:    "LIC of India",
		InsuredName:    "Sourav Das",
		AmountDue:      8400,
		DueDate:        time.Date(2026, time.November, 15, 0, 0, 0, 0, time.UTC),
		DaysLeft:       -6,
		IsOverdue:      true,
		AdvisorName:    "Pradip Saha",
		AdvisorContact: "9830000000",
	}

	overdueHTML := RenderRenewalReminderEmail("Sourav Das", overdueData)
	assert.Contains(t, overdueHTML, "Premium Overdue")
	assert.Contains(t, overdueHTML, "Overdue by 6 Days")
	assert.Contains(t, overdueHTML, "#DC2626") // Red color for overdue
}

func TestHTMLInjectionEscaping(t *testing.T) {
	maliciousName := `<script>alert("hacked")</script>`
	maliciousData := PolicyNotificationData{
		PolicyNo:    `12345"><img src=x onerror=alert(1)>`,
		PlanName:    `<b>Hacked Plan</b>`,
		InsuredName: `Client & Family`,
	}

	html := RenderPolicyCreatedEmail(maliciousName, maliciousData)

	assert.False(t, strings.Contains(html, "<script>"))
	assert.False(t, strings.Contains(html, "<img src=x"))
	assert.Contains(t, html, "&lt;script&gt;")
}
