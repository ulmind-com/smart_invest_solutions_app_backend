package email

import (
	"time"
)

// PolicyNotificationData carries the details needed to render a policy creation/update email.
type PolicyNotificationData struct {
	PolicyType         string    // e.g. "Life Insurance", "Health Insurance", "Motor Insurance", "Fixed Deposit"
	PolicyNo           string    // e.g. "412345678"
	PlanName           string    // e.g. "LIC Jeevan Labh (936)"
	CompanyName        string    // e.g. "LIC of India"
	InsuredName        string    // e.g. "Akash Jana"
	NomineeName        string    // e.g. "Pooja Jana"
	SumAssured         float64   // e.g. 500000
	InstallmentPremium float64   // e.g. 18450
	PaymentMode        string    // e.g. "Yearly", "Monthly"
	NextDueDate        time.Time // Next premium due date
	DOC                time.Time // Date of commencement
	MaturityDate       time.Time // Maturity date
	AdvisorName        string    // Managing advisor
	AdvisorContact     string    // Advisor phone / email
}

// PaymentReceiptData carries the details needed to render a premium payment receipt email.
type PaymentReceiptData struct {
	PolicyType         string    // e.g. "Life Insurance", "Health Insurance"
	PolicyNo           string    // e.g. "412345678"
	PlanName           string    // e.g. "LIC Jeevan Labh"
	CompanyName        string    // e.g. "LIC of India"
	InsuredName        string    // e.g. "Akash Jana"
	AmountPaid         float64   // Installment amount collected
	PaidAt             time.Time // Payment date
	NextDueDate        time.Time // Advanced next due date
	PaymentMode        string    // e.g. "Yearly"
	AdvisorName        string
	AdvisorContact     string
}

// RenewalReminderData carries the details needed to render an upcoming or overdue renewal reminder email.
type RenewalReminderData struct {
	PolicyType     string    // e.g. "Life Insurance", "Health Insurance", "Motor Insurance", "Postal Deposit"
	PolicyNo       string    // Policy / Vehicle / FD Number
	PlanName       string    // Plan name or Vehicle model
	CompanyName    string    // e.g. "LIC of India", "Star Health"
	InsuredName    string    // Insured person
	AmountDue      float64   // Premium or Maturity amount
	DueDate        time.Time // Due date
	DaysLeft       int       // Days left (0 = today, negative = overdue)
	IsOverdue      bool      // Whether the due date has already passed
	PaymentMode    string    // e.g. "Yearly"
	AdvisorName    string
	AdvisorContact string
}
