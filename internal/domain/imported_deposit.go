package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Status values for filtering the imported-deposit inbox. Like the policy inbox, they are derived
// at query time from whether a real fixed_deposits record exists for the account number under the
// same agency — never stored, so the inbox self-heals when a deposit is created or removed by hand.
const (
	ImportedDepositStatusUnclaimed = "unclaimed"
	ImportedDepositStatusLinked    = "linked"
	ImportedDepositStatusAll       = "all"
)

// ImportedDeposit is one account read from a Post Office report, stored so an agency's whole
// deposit book lives in the app — not just the accounts whose holder already has a client login.
//
// Identity is (AgencyID, AccountNo). Account numbers are unique per deposit, while holder names
// repeat constantly in a real report, so the sync never infers "same person" from a name: attaching
// an account to a client is always an explicit admin action.
type ImportedDeposit struct {
	ID bson.ObjectID `bson:"_id,omitempty" json:"id"`
	// AgencyID is the uploading admin's own AdminID — the hard agency boundary for this record.
	AgencyID        string `bson:"agency_id" json:"agency_id"`
	AccountNo       string `bson:"account_no" json:"account_no"`
	HolderName      string `bson:"holder_name" json:"holder_name"`
	JointHolderName string `bson:"joint_holder_name,omitempty" json:"joint_holder_name,omitempty"`
	// Scheme as printed on the report ("5YR MIS", "1TD", "KVP"), plus its parsed code ("MIS").
	Scheme     string `bson:"scheme" json:"scheme"`
	SchemeCode string `bson:"scheme_code,omitempty" json:"scheme_code,omitempty"`
	// TermMonths is taken from the issue and maturity dates when both are printed, since the scheme
	// label on these reports is not always consistent with them.
	TermMonths     int     `bson:"term_months,omitempty" json:"term_months,omitempty"`
	DepositAmount  float64 `bson:"deposit_amount" json:"deposit_amount"`
	MaturityAmount float64 `bson:"maturity_amount,omitempty" json:"maturity_amount,omitempty"`
	// MonthlyIncome is set for monthly-income schemes, where the report prints "585 P.M" in the
	// maturity column: the deposit pays that out every month and returns the principal at maturity.
	MonthlyIncome float64   `bson:"monthly_income,omitempty" json:"monthly_income,omitempty"`
	IssueDate     time.Time `bson:"issue_date" json:"issue_date"`
	MaturityDate  time.Time `bson:"maturity_date,omitempty" json:"maturity_date,omitempty"`
	Remarks       string    `bson:"remarks,omitempty" json:"remarks,omitempty"`
	// ReportName is the uploaded file's name, so an admin can tell which report a row came from.
	ReportName   string    `bson:"report_name,omitempty" json:"report_name,omitempty"`
	FirstSeenAt  time.Time `bson:"first_seen_at" json:"first_seen_at"`
	LastSyncedAt time.Time `bson:"last_synced_at" json:"last_synced_at"`
	CreatedAt    time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time `bson:"updated_at" json:"updated_at"`
}

// ImportedDepositView is an ImportedDeposit enriched, at query time, with whether a client of this
// agency already holds it. IsLinked is true only when the matched deposit's owner belongs to the
// *calling* agency, so an account number held by another agency never leaks that client's name.
type ImportedDepositView struct {
	ImportedDeposit `bson:",inline"`
	IsLinked        bool           `bson:"is_linked" json:"is_linked"`
	LinkedDepositID *bson.ObjectID `bson:"linked_deposit_id,omitempty" json:"linked_deposit_id,omitempty"`
	LinkedUserID    *bson.ObjectID `bson:"linked_user_id,omitempty" json:"linked_user_id,omitempty"`
	LinkedClient    string         `bson:"linked_client,omitempty" json:"linked_client,omitempty"`
}

// LinkImportedDepositDTO attaches an unclaimed imported deposit to a real client account. The
// report carries the money and the dates, so the admin only supplies what it cannot know: whose
// account it is and which family member holds it.
type LinkImportedDepositDTO struct {
	UserID         string `json:"user_id" binding:"required" example:"64f1a2b3c4d5e6f7a8b9c0d1"`
	FamilyMemberID string `json:"family_member_id" binding:"required"`
	// FDName defaults to the scheme printed on the report ("5YR MIS").
	FDName string `json:"fd_name,omitempty"`
	// CompanyName defaults to "India Post" — these reports only ever cover post office schemes.
	CompanyName string `json:"company_name,omitempty"`
	NomineeName string `json:"nominee_name,omitempty"`
	Address     string `json:"address,omitempty"`
	// MaturityDate is required only when the report didn't print one for this account.
	MaturityDate *time.Time `json:"maturity_date,omitempty"`
}

// ImportedDepositRepository defines database operations for the imported-deposit inbox.
type ImportedDepositRepository interface {
	// BulkUpsertFromSync writes one row per account number for the given agency, refreshing the
	// report-sourced fields of rows already present. Returns how many rows were newly created.
	BulkUpsertFromSync(ctx context.Context, records []*ImportedDeposit) (insertedCount int64, err error)
	// FindAll returns the agency's inbox, filtered by derived link status and an optional
	// case-insensitive search across account number and holder names.
	FindAll(ctx context.Context, agencyID, status, search string, page, limit int64) ([]*ImportedDepositView, int64, error)
	FindByID(ctx context.Context, id bson.ObjectID) (*ImportedDeposit, error)
	Delete(ctx context.Context, id bson.ObjectID) error
	// CountByStatus returns (unclaimed, linked) totals for the agency — powers the inbox summary.
	CountByStatus(ctx context.Context, agencyID string) (unclaimed int64, linked int64, err error)
}
