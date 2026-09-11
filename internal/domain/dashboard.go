package domain

import (
	"context"
	"time"
)

// UpcomingPayment represents a single upcoming premium/maturity due date surfaced on the client
// dashboard, merged and sorted chronologically across policy types.
type UpcomingPayment struct {
	Type       string    `json:"type" example:"Life Insurance"` // "Life Insurance", "Health Insurance", "Fixed Deposit", "Motor Insurance"
	EntityName string    `json:"entity_name"`                   // Plan Name, FD Name, or Vehicle No
	Amount     float64   `json:"amount"`                        // 0 for Motor Insurance (no premium/maturity amount on that model)
	DueDate    time.Time `json:"due_date"`                      // next_due_date (premiums), maturity_date (FDs), or date_of_expiry (Motor)
}

// ClientDashboardDTO represents the aggregated summary view shown on a client's dashboard.
type ClientDashboardDTO struct {
	TotalFamilyMembers   int64             `json:"total_family_members"`
	TotalLifePolicies    int64             `json:"total_life_policies"`
	TotalHealthPolicies  int64             `json:"total_health_policies"`
	TotalGeneralPolicies int64             `json:"total_general_policies"`
	TotalFixedDeposits   int64             `json:"total_fixed_deposits"`
	UpcomingPremiums     []UpcomingPayment `json:"upcoming_premiums"` // Life/Health premiums due within the next 30 days
}

// PolicyStats tracks how many financial-instrument records have been formally mapped to the
// agency portfolio (is_mapped: true) vs not, aggregated across Life Insurance, Health Insurance,
// General Insurance, and Fixed Deposits.
type PolicyStats struct {
	Mapped   int64 `json:"mapped"`
	Unmapped int64 `json:"unmapped"`
}

// AdminDashboardDTO represents the aggregated summary view shown on the Admin/Super Admin dashboard.
type AdminDashboardDTO struct {
	TotalActiveClients    int64       `json:"total_active_clients"`
	PendingAccessRequests int64       `json:"pending_access_requests"`
	PolicyStats           PolicyStats `json:"policy_stats"`
}

// DashboardService defines business logic operations for aggregated dashboard views. It is a
// pure orchestrator over existing repositories — it owns no collection of its own.
type DashboardService interface {
	GetClientDashboard(ctx context.Context, userID string) (*ClientDashboardDTO, error)
	// GetAdminDashboard scopes TotalActiveClients and PendingAccessRequests to the caller: a
	// super_admin gets platform-wide totals; a plain admin gets counts limited to their own
	// agency's clients/requests. PolicyStats remains platform-wide for every caller.
	GetAdminDashboard(ctx context.Context, requesterRole, requesterID string) (*AdminDashboardDTO, error)
}
