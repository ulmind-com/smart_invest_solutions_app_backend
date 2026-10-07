package domain

import (
	"context"
	"time"
)

// App-presence states for a client account — the answer to "does this client actually have the app?".
//
// The platform records a sign-in timestamp on every successful login (see
// UserRepository.RecordLogin), so presence is derived from evidence rather than guessed:
//
//   - AppPresenceNoAccess  — the account can't sign in at all (pending approval or switched off),
//     so whether the app is installed is irrelevant until that's fixed.
//   - AppPresenceOnApp     — the account has signed in at least once; LastLoginAt says when.
//   - AppPresenceNotOnApp  — the account can sign in but never has.
const (
	AppPresenceOnApp    = "on_app"
	AppPresenceNotOnApp = "not_on_app"
	AppPresenceNoAccess = "no_access"
)

// ClientMapAgencyUnassigned is the value a caller passes as AgencyID to list the clients that belong
// to no agency at all — accounts a super_admin still has to hand to an admin.
const ClientMapAgencyUnassigned = "unassigned"

// Holdings filters for the client map.
const (
	ClientMapHoldingsWith    = "with"
	ClientMapHoldingsWithout = "without"
)

// ClientMapRow is one line of the client map: a client, who manages them, what they hold, and
// whether they are actually using the app.
type ClientMapRow struct {
	UserID string `bson:"user_id" json:"user_id"`
	Name   string `bson:"name" json:"name"`
	Email  string `bson:"email" json:"email"`
	Phone  string `bson:"phone,omitempty" json:"phone,omitempty"`

	// AgencyID is the Agency ID (an admin's AdminID) this client registered under, and AgencyName
	// the name of that admin. Both are empty when the client belongs to no agency yet — which only
	// a super_admin ever sees, since a plain admin's view is scoped to their own agency.
	AgencyID   string `bson:"agency_id,omitempty" json:"agency_id,omitempty"`
	AgencyName string `bson:"agency_name,omitempty" json:"agency_name,omitempty"`

	IsActive        bool       `bson:"is_active" json:"is_active"`
	IsEmailVerified bool       `bson:"is_email_verified" json:"is_email_verified"`
	LastLoginAt     *time.Time `bson:"last_login_at,omitempty" json:"last_login_at,omitempty"`
	LoginCount      int        `bson:"login_count" json:"login_count"`
	// AppStatus is one of the AppPresence constants, derived from the three fields above.
	AppStatus string `bson:"app_status" json:"app_status"`

	LifeCount    int `bson:"life_count" json:"life_count"`
	HealthCount  int `bson:"health_count" json:"health_count"`
	MotorCount   int `bson:"motor_count" json:"motor_count"`
	DepositCount int `bson:"deposit_count" json:"deposit_count"`
	// PolicyCount is life + health + motor; TotalCount adds deposits. Both are computed server-side
	// so the app, a report and any future export always agree on the same arithmetic.
	PolicyCount int `bson:"policy_count" json:"policy_count"`
	TotalCount  int `bson:"total_count" json:"total_count"`
	// AgencyManagedCount is how many of those records the agency maintains rather than the client
	// (the managed_by flag the client app shows as "Managed by advisor").
	AgencyManagedCount int `bson:"agency_managed_count" json:"agency_managed_count"`

	JoinedAt time.Time `bson:"created_at" json:"joined_at"`
}

// ClientMapSummary totals the whole filtered scope — not just the page on screen — so the counts
// don't change as the reader pages through or switches the status filter.
type ClientMapSummary struct {
	Clients         int64 `bson:"clients" json:"clients"`
	OnApp           int64 `bson:"on_app" json:"on_app"`
	NotOnApp        int64 `bson:"not_on_app" json:"not_on_app"`
	NoAccess        int64 `bson:"no_access" json:"no_access"`
	WithoutHoldings int64 `bson:"without_holdings" json:"without_holdings"`
	Policies        int64 `bson:"policies" json:"policies"`
	Deposits        int64 `bson:"deposits" json:"deposits"`
	// Unassigned counts clients with no Agency ID — super_admin housekeeping, always 0 for an admin
	// (whose scope is their own agency by definition).
	Unassigned int64 `bson:"unassigned" json:"unassigned"`
}

// ClientMapQuery is one request for a page of the map. Every field is optional; the service clamps
// and normalizes it before it reaches the database.
type ClientMapQuery struct {
	// AgencyID restricts the rows to one agency, or to the unassigned clients when set to
	// ClientMapAgencyUnassigned. Ignored for a plain admin, who is always scoped to their own.
	AgencyID string
	Search   string
	// AppStatus is one of the AppPresence constants, or "" for every status.
	AppStatus string
	// Holdings is ClientMapHoldingsWith / ClientMapHoldingsWithout, or "" for every client.
	Holdings string
	Page     int64
	Limit    int64
}

// ClientMapResult is a page of the map plus the totals for the whole filtered scope.
type ClientMapResult struct {
	Items   []*ClientMapRow   `json:"items"`
	Summary *ClientMapSummary `json:"summary"`
	Total   int64             `json:"total"`
}

// ClientMapPage is what the API returns: the rows, the scope-wide summary and the paging the reader
// needs to walk the rest of the table.
type ClientMapPage struct {
	Items      []*ClientMapRow   `json:"items"`
	Summary    *ClientMapSummary `json:"summary"`
	Total      int64             `json:"total"`
	Page       int64             `json:"page"`
	Limit      int64             `json:"limit"`
	TotalPages int64             `json:"total_pages"`
}

// ClientMapRepository reads the client map.
type ClientMapRepository interface {
	// FindClientMap returns one page of client rows and the summary for the whole filtered scope.
	// Rows are ordered by name so the table reads like a directory.
	FindClientMap(ctx context.Context, query ClientMapQuery) (*ClientMapResult, error)
}

// ClientMapService is the agency-scoped client map: which client holds which policies, which admin
// they sit under, and who is actually on the app.
type ClientMapService interface {
	// GetClientMap answers for the caller: a super_admin sees every client and may filter by agency,
	// while a plain admin only ever sees the clients of their own agency.
	GetClientMap(ctx context.Context, requesterRole, requesterID string, query ClientMapQuery) (*ClientMapResult, error)
}
