package domain

import (
	"context"
	"time"
)

// What kind of record a renewal row came from.
const (
	RenewalKindLife    = "life"
	RenewalKindHealth  = "health"
	RenewalKindMotor   = "motor"
	RenewalKindDeposit = "deposit"
)

// IsValidRenewalKind reports whether kind is one of the four instruments.
func IsValidRenewalKind(kind string) bool {
	switch kind {
	case RenewalKindLife, RenewalKindHealth, RenewalKindMotor, RenewalKindDeposit:
		return true
	}
	return false
}

// Windows a renewal list can be narrowed to.
const (
	// RenewalWindowOverdue is only what has already passed and still needs acting on.
	RenewalWindowOverdue = "overdue"
	RenewalWindow7       = "7"
	RenewalWindow30      = "30"
	RenewalWindow90      = "90"
	// RenewalWindowAll is everything the service looks at: overdue plus the next 90 days.
	RenewalWindowAll = "all"
)

// RenewalHorizonDays is how far ahead the service ever looks. Everything beyond this is somebody
// else's problem this quarter, and fetching it would make the list unreadable rather than useful.
const RenewalHorizonDays = 90

// RenewalRow is one thing that needs acting on: a premium due, a policy about to expire, or a
// deposit about to mature — with the client who holds it and the admin whose agency they sit in.
type RenewalRow struct {
	// Kind is one of the RenewalKind constants; RecordID identifies the underlying record so the app
	// can open its detail screen.
	Kind     string `json:"kind"`
	RecordID string `json:"record_id"`

	// Title is what the row is called (plan name, vehicle number, deposit name) and Reference the
	// number that identifies it (policy number, vehicle number, account number).
	Title     string `json:"title"`
	Reference string `json:"reference,omitempty"`
	Company   string `json:"company,omitempty"`
	// InsuredName is the family member the record is filed against, where there is one.
	InsuredName string `json:"insured_name,omitempty"`

	// Amount is the premium instalment, or the maturity value for a deposit. Zero for motor, which
	// carries no premium on this platform.
	Amount float64 `json:"amount"`
	// Label says what the date means for this kind, e.g. "Premium due" or "Policy expires".
	Label   string    `json:"label"`
	DueDate time.Time `json:"due_date"`
	// DaysLeft counts whole days on the Indian calendar: 0 is today, negative is overdue.
	DaysLeft  int  `json:"days_left"`
	IsOverdue bool `json:"is_overdue"`

	ClientID    string `json:"client_id"`
	ClientName  string `json:"client_name"`
	ClientPhone string `json:"client_phone,omitempty"`

	// AgencyID is the Agency ID / Admin ID of the admin managing this client, and AgencyName that
	// admin's name. Both empty when the client belongs to no agency — which a super admin needs to
	// see, because nobody is currently chasing that renewal.
	AgencyID   string `json:"agency_id,omitempty"`
	AgencyName string `json:"agency_name,omitempty"`
	// ManagedBy is who maintains the record itself — see the ManagedBy constants.
	ManagedBy string `json:"managed_by,omitempty"`
}

// RenewalSummary counts the whole scope the caller is looking at, not the page — so the tallies hold
// still while the reader switches windows and instruments.
type RenewalSummary struct {
	Overdue int64 `json:"overdue"`
	// DueIn7 / DueIn30 / DueIn90 are cumulative from today, so 7 is included in 30.
	DueIn7  int64 `json:"due_in_7"`
	DueIn30 int64 `json:"due_in_30"`
	DueIn90 int64 `json:"due_in_90"`

	Life     int64 `json:"life"`
	Health   int64 `json:"health"`
	Motor    int64 `json:"motor"`
	Deposits int64 `json:"deposits"`

	// AmountOverdue and AmountDueIn30 total the money attached to those rows — the premium that
	// hasn't been collected, and what is coming up.
	AmountOverdue float64 `json:"amount_overdue"`
	AmountDueIn30 float64 `json:"amount_due_in_30"`

	// Unassigned counts rows whose client belongs to no agency. Always 0 for a plain admin.
	Unassigned int64 `json:"unassigned"`
}

// RenewalQuery is one request for a page of the renewal list.
type RenewalQuery struct {
	// AgencyID narrows to one agency, or to ClientMapAgencyUnassigned for clients with no agency.
	// Honoured only for a super_admin; a plain admin is always pinned to their own.
	AgencyID string
	// Kind is one of the RenewalKind constants, or "" for every instrument.
	Kind string
	// Window is one of the RenewalWindow constants; "" means RenewalWindowAll.
	Window string
	// Search matches the client, the title and the reference number.
	Search string
	Page   int64
	Limit  int64
}

// RenewalPage is a page of the renewal list plus the summary for the whole filtered scope.
type RenewalPage struct {
	Items      []*RenewalRow   `json:"items"`
	Summary    *RenewalSummary `json:"summary"`
	Total      int64           `json:"total"`
	Page       int64           `json:"page"`
	Limit      int64           `json:"limit"`
	TotalPages int64           `json:"total_pages"`
}

// RenewalRepository reads the records that fall due inside the horizon.
//
// Each method returns only records whose own date sits in [from, to] — the filtering happens in the
// database rather than by reading a whole book into memory, and the horizon keeps the result bounded
// however large the agency gets. agencyID narrows by the owning client's agency, with the
// ClientMapAgencyUnassigned sentinel meaning "clients with no agency".
type RenewalRepository interface {
	LifeDue(ctx context.Context, from, to time.Time, agencyID string) ([]*LifeInsuranceWithCustomer, error)
	HealthDue(ctx context.Context, from, to time.Time, agencyID string) ([]*HealthInsuranceWithCustomer, error)
	// MotorExpiring takes the window as YYYY-MM-DD strings: motor expiry is stored as a date string,
	// which compares correctly in that format.
	MotorExpiring(ctx context.Context, fromISO, toISO, agencyID string) ([]*GeneralInsuranceWithCustomer, error)
	DepositsMaturing(ctx context.Context, from, to time.Time, agencyID string) ([]*FixedDepositWithCustomer, error)
}

// RenewalService is the agency's renewal book: what is due, when, for whom, and under which admin.
type RenewalService interface {
	// GetRenewals returns one page of the renewal list for this caller. A super_admin sees the whole
	// platform and may narrow to one agency; a plain admin only ever sees their own agency's book.
	GetRenewals(ctx context.Context, requesterRole, requesterID string, query RenewalQuery) (*RenewalPage, error)
}
