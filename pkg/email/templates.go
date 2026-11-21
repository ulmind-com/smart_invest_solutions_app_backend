package email

import (
	"fmt"
	"html"
	"time"
)

func formatCurrency(amount float64) string {
	if amount <= 0 {
		return "—"
	}
	// Format with Indian grouping style or clean comma format
	return fmt.Sprintf("₹%.2f", amount)
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("02 Jan 2006")
}

// buildBaseEmailLayout wraps email body content with SmartWealth's authoritative blue design system.
func buildBaseEmailLayout(
	title string,
	badgeText, badgeBg, badgeColor string,
	headline, subheadline string,
	contentHTML string,
	ctaText, ctaURL string,
	advisorName, advisorContact string,
) string {
	if ctaURL == "" {
		ctaURL = "https://smartwealth.ulmind.com"
	}

	advisorSection := ""
	if advisorName != "" || advisorContact != "" {
		advisorSection = fmt.Sprintf(`
          <tr>
            <td style="padding: 18px 28px; background-color: #F8FAFC; border-top: 1px solid #E2E8F0;">
              <table role="presentation" border="0" cellpadding="0" cellspacing="0" width="100%%">
                <tr>
                  <td valign="top" style="width: 36px; padding-right: 12px;">
                    <div style="width: 36px; height: 36px; border-radius: 18px; background-color: #DBEAFE; text-align: center; line-height: 36px; font-size: 16px;">
                      👔
                    </div>
                  </td>
                  <td valign="top">
                    <p style="margin: 0; font-size: 11px; text-transform: uppercase; color: #64748B; font-weight: 700; letter-spacing: 0.5px;">Your Assigned Advisor / Agency</p>
                    <p style="margin: 2px 0 0 0; font-size: 14px; color: #0F172A; font-weight: 700;">%s</p>
                    %s
                  </td>
                </tr>
              </table>
            </td>
          </tr>
		`,
			html.EscapeString(advisorName),
			func() string {
				if advisorContact != "" {
					return fmt.Sprintf(`<p style="margin: 2px 0 0 0; font-size: 13px; color: #2563EB;">📞 %s</p>`, html.EscapeString(advisorContact))
				}
				return ""
			}(),
		)
	}

	ctaSection := ""
	if ctaText != "" {
		ctaSection = fmt.Sprintf(`
          <tr>
            <td align="center" style="padding: 10px 28px 28px 28px;">
              <table role="presentation" border="0" cellpadding="0" cellspacing="0">
                <tr>
                  <td align="center" style="border-radius: 10px; background: linear-gradient(135deg, #1D4ED8 0%%, #2563EB 100%%);">
                    <a href="%s" target="_blank" style="display: inline-block; padding: 13px 28px; font-size: 14px; font-weight: 700; color: #FFFFFF; text-decoration: none; border-radius: 10px; letter-spacing: 0.3px;">
                      %s →
                    </a>
                  </td>
                </tr>
              </table>
            </td>
          </tr>
		`, ctaURL, html.EscapeString(ctaText))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>%s</title>
  <style>
    body { margin: 0; padding: 0; width: 100%% !important; background-color: #F1F5F9; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; color: #1E293B; }
    table { border-collapse: separate; }
    @media screen and (max-width: 600px) {
      .container { width: 100%% !important; border-radius: 0 !important; }
      .content-padding { padding: 20px 16px !important; }
      .header-padding { padding: 24px 16px !important; }
      .data-row td { display: block !important; width: 100%% !important; padding: 8px 12px !important; }
    }
  </style>
</head>
<body style="background-color: #F1F5F9; margin: 0; padding: 20px 0;">
  <table role="presentation" border="0" cellpadding="0" cellspacing="0" width="100%%">
    <tr>
      <td align="center">
        <!-- Main Card Container -->
        <table role="presentation" class="container" border="0" cellpadding="0" cellspacing="0" width="580" style="max-width: 580px; background-color: #FFFFFF; border-radius: 16px; overflow: hidden; box-shadow: 0 4px 20px rgba(10, 31, 68, 0.08); border: 1px solid #E2E8F0;">
          
          <!-- Header (App Royal Navy Brand) -->
          <tr>
            <td class="header-padding" style="background: linear-gradient(135deg, #0A1F44 0%%, #153A82 50%%, #1D4ED8 100%%); padding: 30px 24px; text-align: center;">
              <table role="presentation" border="0" cellpadding="0" cellspacing="0" width="100%%">
                <tr>
                  <td align="center">
                    <span style="display: inline-block; background: rgba(255, 255, 255, 0.15); border: 1px solid rgba(255, 255, 255, 0.25); border-radius: 20px; padding: 3px 12px; color: #FCD34D; font-size: 11px; font-weight: 700; letter-spacing: 1px; text-transform: uppercase;">
                      ✦ SMART INVEST SOLUTIONS
                    </span>
                    <h1 style="margin: 8px 0 0 0; color: #FFFFFF; font-size: 24px; font-weight: 800; letter-spacing: -0.5px;">
                      SmartWealth
                    </h1>
                    <p style="margin: 3px 0 0 0; color: #93C5FD; font-size: 13px; font-weight: 500;">
                      LIC & Comprehensive Wealth Portfolio
                    </p>
                  </td>
                </tr>
              </table>
            </td>
          </tr>

          <!-- Notification Title Banner -->
          <tr>
            <td class="content-padding" style="padding: 24px 28px 8px 28px; text-align: left;">
              <span style="display: inline-block; padding: 4px 10px; border-radius: 6px; font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.5px; background-color: %s; color: %s;">
                %s
              </span>
              <h2 style="margin: 10px 0 4px 0; color: #0B1729; font-size: 20px; font-weight: 700;">
                %s
              </h2>
              <p style="margin: 0; color: #64748B; font-size: 14px; line-height: 1.5;">
                %s
              </p>
            </td>
          </tr>

          <!-- Dynamic Content Body -->
          <tr>
            <td class="content-padding" style="padding: 16px 28px 20px 28px;">
              %s
            </td>
          </tr>

          <!-- Action CTA -->
          %s

          <!-- Assigned Advisor Section -->
          %s

          <!-- Security Note -->
          <tr>
            <td style="padding: 16px 28px; background-color: #EFF6FF; border-top: 1px solid #DBEAFE; font-size: 12px; color: #1E3A8A; line-height: 1.5;">
              🔒 <strong>Security Advisory:</strong> Smart Invest Solutions will never ask for your account password, PIN, or confidential banking credentials via email. Always sign in through the official SmartWealth app.
            </td>
          </tr>

          <!-- Footer (Regulatory & Play Store Anti-Spam Compliant) -->
          <tr>
            <td style="padding: 22px 28px; background-color: #FFFFFF; text-align: center; font-size: 11px; color: #94A3B8; line-height: 1.6; border-top: 1px solid #F1F5F9;">
              <p style="margin: 0 0 6px 0;">
                You are receiving this automated transactional notification because you are a registered policyholder at Smart Invest Solutions.
              </p>
              <p style="margin: 0;">
                &copy; %d Smart Invest Solutions. All rights reserved. &bull; Powered by SmartWealth
              </p>
            </td>
          </tr>

        </table>
      </td>
    </tr>
  </table>
</body>
</html>`,
		html.EscapeString(title),
		badgeBg,
		badgeColor,
		html.EscapeString(badgeText),
		headline,
		subheadline,
		contentHTML,
		ctaSection,
		advisorSection,
		time.Now().Year(),
	)
}

// RenderPolicyCreatedEmail generates a template when a policy or deposit is created or linked.
func RenderPolicyCreatedEmail(clientName string, data PolicyNotificationData) string {
	clientName = html.EscapeString(clientName)
	policyType := data.PolicyType
	if policyType == "" {
		policyType = "Insurance Policy"
	}

	badgeText := "🎉 Policy Added & Linked"
	headline := fmt.Sprintf("Hello %s, your policy has been linked!", clientName)
	subheadline := fmt.Sprintf("Your %s record has been successfully added to your SmartWealth portfolio.", policyType)

	tableRows := fmt.Sprintf(`
      <table role="presentation" border="0" cellpadding="0" cellspacing="0" width="100%%" style="background-color: #F8FAFC; border: 1px solid #E2E8F0; border-radius: 12px; overflow: hidden; margin-bottom: 16px;">
        <tr style="border-bottom: 1px solid #E2E8F0;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700; width: 40%%;">Policy / Acc No.</td>
          <td style="padding: 12px 16px; font-size: 15px; color: #0F172A; font-weight: 700; font-family: monospace;">%s</td>
        </tr>
        <tr style="border-bottom: 1px solid #E2E8F0; background-color: #FFFFFF;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Plan / Scheme</td>
          <td style="padding: 12px 16px; font-size: 14px; color: #0F172A; font-weight: 600;">%s (%s)</td>
        </tr>
        <tr style="border-bottom: 1px solid #E2E8F0;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Life / Member Insured</td>
          <td style="padding: 12px 16px; font-size: 14px; color: #0F172A; font-weight: 600;">%s</td>
        </tr>
        %s
        <tr style="border-bottom: 1px solid #E2E8F0; background-color: #FFFFFF;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Installment Premium</td>
          <td style="padding: 12px 16px; font-size: 16px; color: #1D4ED8; font-weight: 800;">%s <span style="font-size: 12px; font-weight: 500; color: #64748B;">(%s)</span></td>
        </tr>
        <tr style="background-color: #F8FAFC;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Next Premium Due Date</td>
          <td style="padding: 12px 16px; font-size: 14px; color: #059669; font-weight: 700;">📅 %s</td>
        </tr>
      </table>
      <p style="margin: 0; font-size: 13px; color: #475569; line-height: 1.5;">
        You can track this policy's due dates, download policy documents, and inspect family coverage details anytime directly inside your SmartWealth App.
      </p>
	`,
		html.EscapeString(data.PolicyNo),
		html.EscapeString(data.PlanName),
		html.EscapeString(data.CompanyName),
		html.EscapeString(data.InsuredName),
		func() string {
			if data.SumAssured > 0 {
				return fmt.Sprintf(`
        <tr style="border-bottom: 1px solid #E2E8F0; background-color: #FFFFFF;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Sum Assured</td>
          <td style="padding: 12px 16px; font-size: 14px; color: #0F172A; font-weight: 700;">%s</td>
        </tr>`, formatCurrency(data.SumAssured))
			}
			return ""
		}(),
		formatCurrency(data.InstallmentPremium),
		html.EscapeString(data.PaymentMode),
		formatDate(data.NextDueDate),
	)

	return buildBaseEmailLayout(
		"Policy Linked — Smart Invest Solutions",
		badgeText, "#D1FAE5", "#059669",
		headline, subheadline,
		tableRows,
		"View Policy in SmartWealth",
		"https://smartwealth.ulmind.com",
		data.AdvisorName, data.AdvisorContact,
	)
}

// RenderPolicyUpdatedEmail generates an email when an existing policy is updated.
func RenderPolicyUpdatedEmail(clientName string, data PolicyNotificationData) string {
	clientName = html.EscapeString(clientName)
	badgeText := "📝 Policy Details Updated"
	headline := fmt.Sprintf("Hello %s, your policy details have been updated", clientName)
	subheadline := fmt.Sprintf("Updates have been recorded for your %s (Policy #%s).", data.PolicyType, html.EscapeString(data.PolicyNo))

	tableRows := fmt.Sprintf(`
      <table role="presentation" border="0" cellpadding="0" cellspacing="0" width="100%%" style="background-color: #F8FAFC; border: 1px solid #E2E8F0; border-radius: 12px; overflow: hidden; margin-bottom: 16px;">
        <tr style="border-bottom: 1px solid #E2E8F0;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700; width: 40%%;">Policy Number</td>
          <td style="padding: 12px 16px; font-size: 15px; color: #0F172A; font-weight: 700; font-family: monospace;">%s</td>
        </tr>
        <tr style="border-bottom: 1px solid #E2E8F0; background-color: #FFFFFF;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Plan Name</td>
          <td style="padding: 12px 16px; font-size: 14px; color: #0F172A; font-weight: 600;">%s (%s)</td>
        </tr>
        <tr style="border-bottom: 1px solid #E2E8F0;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Insured Person</td>
          <td style="padding: 12px 16px; font-size: 14px; color: #0F172A; font-weight: 600;">%s</td>
        </tr>
        <tr style="border-bottom: 1px solid #E2E8F0; background-color: #FFFFFF;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Premium & Mode</td>
          <td style="padding: 12px 16px; font-size: 15px; color: #1D4ED8; font-weight: 700;">%s (%s)</td>
        </tr>
        <tr style="background-color: #F8FAFC;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Next Due Date</td>
          <td style="padding: 12px 16px; font-size: 14px; color: #059669; font-weight: 700;">📅 %s</td>
        </tr>
      </table>
      <p style="margin: 0; font-size: 13px; color: #475569; line-height: 1.5;">
        If you did not request or expect this change, please reach out to your assigned advisor immediately.
      </p>
	`,
		html.EscapeString(data.PolicyNo),
		html.EscapeString(data.PlanName),
		html.EscapeString(data.CompanyName),
		html.EscapeString(data.InsuredName),
		formatCurrency(data.InstallmentPremium),
		html.EscapeString(data.PaymentMode),
		formatDate(data.NextDueDate),
	)

	return buildBaseEmailLayout(
		"Policy Updated — Smart Invest Solutions",
		badgeText, "#EDE9FE", "#6D28D9",
		headline, subheadline,
		tableRows,
		"View in SmartWealth",
		"https://smartwealth.ulmind.com",
		data.AdvisorName, data.AdvisorContact,
	)
}

// RenderPaymentReceiptEmail generates an acknowledgment email when premium payment is recorded.
func RenderPaymentReceiptEmail(clientName string, data PaymentReceiptData) string {
	clientName = html.EscapeString(clientName)
	badgeText := "✅ Premium Payment Acknowledged"
	headline := fmt.Sprintf("Payment Received, %s!", clientName)
	subheadline := fmt.Sprintf("Your premium installment for %s (Policy #%s) has been successfully recorded.", data.PlanName, html.EscapeString(data.PolicyNo))

	tableRows := fmt.Sprintf(`
      <table role="presentation" border="0" cellpadding="0" cellspacing="0" width="100%%" style="background-color: #F8FAFC; border: 1px solid #E2E8F0; border-radius: 12px; overflow: hidden; margin-bottom: 16px;">
        <tr style="border-bottom: 1px solid #E2E8F0;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700; width: 40%%;">Amount Collected</td>
          <td style="padding: 12px 16px; font-size: 18px; color: #059669; font-weight: 800;">%s</td>
        </tr>
        <tr style="border-bottom: 1px solid #E2E8F0; background-color: #FFFFFF;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Policy Number</td>
          <td style="padding: 12px 16px; font-size: 15px; color: #0F172A; font-weight: 700; font-family: monospace;">%s</td>
        </tr>
        <tr style="border-bottom: 1px solid #E2E8F0;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Life Insured</td>
          <td style="padding: 12px 16px; font-size: 14px; color: #0F172A; font-weight: 600;">%s</td>
        </tr>
        <tr style="border-bottom: 1px solid #E2E8F0; background-color: #FFFFFF;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Date of Payment</td>
          <td style="padding: 12px 16px; font-size: 14px; color: #0F172A; font-weight: 600;">%s</td>
        </tr>
        <tr style="background-color: #F8FAFC;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">New Next Due Date</td>
          <td style="padding: 12px 16px; font-size: 14px; color: #2563EB; font-weight: 800;">📅 %s</td>
        </tr>
      </table>
      <div style="background-color: #F0FDF4; border: 1px solid #BBF7D0; border-radius: 8px; padding: 12px; font-size: 13px; color: #166534; line-height: 1.4;">
        ✔ Your policy status remains in active force. The official premium payment receipt can be downloaded inside your E-Vault documents tab.
      </div>
	`,
		formatCurrency(data.AmountPaid),
		html.EscapeString(data.PolicyNo),
		html.EscapeString(data.InsuredName),
		formatDate(data.PaidAt),
		formatDate(data.NextDueDate),
	)

	return buildBaseEmailLayout(
		"Premium Payment Acknowledged — Smart Invest Solutions",
		badgeText, "#D1FAE5", "#059669",
		headline, subheadline,
		tableRows,
		"Open SmartWealth E-Vault",
		"https://smartwealth.ulmind.com",
		data.AdvisorName, data.AdvisorContact,
	)
}

// RenderRenewalReminderEmail generates a renewal reminder email for due or overdue policies/deposits.
func RenderRenewalReminderEmail(clientName string, data RenewalReminderData) string {
	clientName = html.EscapeString(clientName)

	badgeText := "⏳ Premium Due Soon"
	badgeBg := "#FEF3C7"
	badgeColor := "#D97706"
	dueBadge := fmt.Sprintf("Due in %d Days", data.DaysLeft)
	if data.DaysLeft == 0 {
		dueBadge = "Due Today"
	} else if data.IsOverdue || data.DaysLeft < 0 {
		badgeText = "⚠️ Premium Overdue"
		badgeBg = "#FEE2E2"
		badgeColor = "#DC2626"
		dueBadge = fmt.Sprintf("Overdue by %d Days", -data.DaysLeft)
	}

	headline := fmt.Sprintf("Renewal Notice for %s", html.EscapeString(data.PlanName))
	subheadline := fmt.Sprintf("Dear %s, your upcoming premium installment of %s is due for payment.", clientName, formatCurrency(data.AmountDue))

	tableRows := fmt.Sprintf(`
      <table role="presentation" border="0" cellpadding="0" cellspacing="0" width="100%%" style="background-color: #F8FAFC; border: 1px solid #E2E8F0; border-radius: 12px; overflow: hidden; margin-bottom: 16px;">
        <tr style="border-bottom: 1px solid #E2E8F0;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700; width: 40%%;">Amount Due</td>
          <td style="padding: 12px 16px; font-size: 18px; color: %s; font-weight: 800;">%s</td>
        </tr>
        <tr style="border-bottom: 1px solid #E2E8F0; background-color: #FFFFFF;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Due Date & Status</td>
          <td style="padding: 12px 16px; font-size: 14px; font-weight: 700; color: #0F172A;">
            📅 %s &nbsp;<span style="display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 11px; font-weight: 700; background-color: %s; color: %s;">%s</span>
          </td>
        </tr>
        <tr style="border-bottom: 1px solid #E2E8F0;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Policy / Instrument</td>
          <td style="padding: 12px 16px; font-size: 14px; color: #0F172A; font-weight: 700;">%s</td>
        </tr>
        <tr style="border-bottom: 1px solid #E2E8F0; background-color: #FFFFFF;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Policy Number</td>
          <td style="padding: 12px 16px; font-size: 15px; color: #0F172A; font-weight: 700; font-family: monospace;">%s</td>
        </tr>
        <tr style="background-color: #F8FAFC;">
          <td style="padding: 12px 16px; font-size: 12px; text-transform: uppercase; color: #64748B; font-weight: 700;">Insured Person</td>
          <td style="padding: 12px 16px; font-size: 14px; color: #0F172A; font-weight: 600;">%s</td>
        </tr>
      </table>

      <div style="background-color: #FFFBEB; border: 1px solid #FDE68A; border-radius: 8px; padding: 12px; font-size: 13px; color: #92400E; line-height: 1.5; margin-bottom: 12px;">
        💡 <strong>Grace Period Information:</strong> For standard LIC life policies, a grace period of 30 days is allowed for Yearly, Half-Yearly, and Quarterly payment modes (15 days for Monthly mode). Timely renewal guarantees continuous life risk cover and accrued bonus benefits.
      </div>
      <p style="margin: 0; font-size: 13px; color: #475569; line-height: 1.5;">
        You can pay your premium online via the official portal, through UPI/NetBanking, or contact your agency advisor for assistance.
      </p>
	`,
		func() string {
			if data.IsOverdue {
				return "#DC2626"
			}
			return "#1D4ED8"
		}(),
		formatCurrency(data.AmountDue),
		formatDate(data.DueDate),
		badgeBg, badgeColor, dueBadge,
		html.EscapeString(data.PlanName),
		html.EscapeString(data.PolicyNo),
		html.EscapeString(data.InsuredName),
	)

	subjectKind := "Upcoming Renewal Reminder"
	if data.IsOverdue {
		subjectKind = "Urgent: Premium Overdue Notice"
	}

	return buildBaseEmailLayout(
		fmt.Sprintf("%s — Smart Invest Solutions", subjectKind),
		badgeText, badgeBg, badgeColor,
		headline, subheadline,
		tableRows,
		"Pay or Check in SmartWealth",
		"https://smartwealth.ulmind.com",
		data.AdvisorName, data.AdvisorContact,
	)
}
