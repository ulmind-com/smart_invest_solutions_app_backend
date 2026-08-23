package domain

import (
	"context"
	"time"
)

// UpcomingPayment represents a single upcoming premium/maturity due date surfaced on the client
// dashboard, merged and sorted chronologically across policy types.
type UpcomingPayment struct {
	Type       string    `json:"type" example:"Life Insurance"` // "Life Insurance", "Health Insurance", "Fixed Deposit"
	EntityName string    `json:"entity_name"`                   // Plan Name or FD Name
	Amount     float64   `json:"amount"`
	DueDate    time.Time `json:"due_date"` // Maps to next_due_date (premiums) or maturity_date (FDs)
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
	GetAdminDashboard(ctx context.Context) (*AdminDashboardDTO, error)
}
