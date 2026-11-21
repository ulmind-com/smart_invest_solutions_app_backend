package domain

import (
	"context"
	"time"
)

// CronExecutionResult contains the detailed report of a completed background maintenance run.
type CronExecutionResult struct {
	Status                string                 `json:"status"`
	ExecutedAt            time.Time              `json:"executed_at"`
	DurationMs            int64                  `json:"duration_ms"`
	AdminExpiryAlertsSent int                    `json:"admin_expiry_alerts_sent"`
	AdminAccountsChecked  int                    `json:"admin_accounts_checked"`
	ActiveAdminsExpired   int                    `json:"active_admins_expired"`
	ClientRenewalAlertsSent int                  `json:"client_renewal_alerts_sent"`
	RenewalsSummary       map[string]interface{} `json:"renewals_summary"`
	Message               string                 `json:"message"`
}

// CronService provides scheduled maintenance operations across the platform.
type CronService interface {
	RunDailyMaintenance(ctx context.Context) (*CronExecutionResult, error)
}
