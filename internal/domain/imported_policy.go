package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Status values for filtering the imported-policy inbox. These are *derived* at query time from
// whether a real life_insurances record exists for the policy number under the same agency — they
// are never stored on the imported policy itself, so the inbox self-heals if a policy is created
// manually, deleted, or re-linked without needing a follow-up write.
const (
	ImportedPolicyStatusUnclaimed = "unclaimed"
	ImportedPolicyStatusLinked    = "linked"
	ImportedPolicyStatusAll       = "all"
)

// ImportedPolicy is one row of an LIC Premium Due List PDF, stored durably so the agency's whole
// book of business lives in the app — not just the policies that happen to have a client account
// already. Rows whose policy number has no matching life_insurances record are "unclaimed": they
// sit in the admin's inbox until that client signs up (or is found) and the admin links them.
//
// Identity is (AgencyID, PolicyNo) and nothing else. LIC policy numbers are unique per policy,
// whereas assured names collide constantly in real due lists (a single sample list had the same
// name on four different policies), so the sync never infers "same person" from a name — linking a
// policy to a client account is always an explicit admin action.
type ImportedPolicy struct {
	ID bson.ObjectID `bson:"_id,omitempty" json:"id"`
	// AgencyID is the uploading admin's own AdminID — the hard agency boundary for this record.
	AgencyID    string    `bson:"agency_id" json:"agency_id"`
	PolicyNo    string    `bson:"policy_no" json:"policy_no"`
	AssuredName string    `bson:"assured_name" json:"assured_name"`
	DOC         time.Time `bson:"doc" json:"doc"`
	// PlanCode and Term come from the PDF's "Pln/Tm" column (e.g. "714/18" → plan 714, term 18).
	PlanCode string `bson:"plan_code" json:"plan_code"`
	Term     int    `bson:"term" json:"term"`
	// Mode is normalized to the same vocabulary as PremiumDetails.PaymentMode.
	Mode string `bson:"mode" json:"mode"`
	// FUP ("first unpaid premium") is kept exactly as printed, MM/YYYY.
	FUP string `bson:"fup" json:"fup"`
	// Flag is the PDF's "Flg" column: FY (first year), ST (2nd/3rd year), LP (last premium),
	// MT (matures after this due), or empty.
	Flag                string    `bson:"flag,omitempty" json:"flag,omitempty"`
	InstallmentPremium  float64   `bson:"installment_premium" json:"installment_premium"`
	DueCount            int       `bson:"due_count" json:"due_count"`
	TotalPremium        float64   `bson:"total_premium" json:"total_premium"`
	EstimatedCommission float64   `bson:"estimated_commission" json:"estimated_commission"`
	NextDueDate         time.Time `bson:"next_due_date" json:"next_due_date"`
	// AgentCode and DueMonth come from the PDF header — useful when an agency works more than one
	// LIC agent code, and for telling at a glance how fresh a row is.
	AgentCode    string    `bson:"agent_code,omitempty" json:"agent_code,omitempty"`
	DueMonth     string    `bson:"due_month,omitempty" json:"due_month,omitempty"`
	FirstSeenAt  time.Time `bson:"first_seen_at" json:"first_seen_at"`
	LastSyncedAt time.Time `bson:"last_synced_at" json:"last_synced_at"`
	CreatedAt    time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time `bson:"updated_at" json:"updated_at"`
}

// ImportedPolicyView is an ImportedPolicy enriched, at query time, with whether it is already held
// by a client of this agency and by whom. IsLinked is true only when the matched policy's owner
// belongs to the *calling* agency — a policy number held by another agency's client reads as
// unclaimed here and never exposes that client's name across the agency boundary.
type ImportedPolicyView struct {
	ImportedPolicy `bson:",inline"`
	IsLinked       bool           `bson:"is_linked" json:"is_linked"`
	LinkedPolicyID *bson.ObjectID `bson:"linked_policy_id,omitempty" json:"linked_policy_id,omitempty"`
	LinkedUserID   *bson.ObjectID `bson:"linked_user_id,omitempty" json:"linked_user_id,omitempty"`
	LinkedClient   string         `bson:"linked_client,omitempty" json:"linked_client,omitempty"`
}

// LinkImportedPolicyDTO is the payload for attaching an unclaimed imported policy to a real client
// account. SumAssured and NomineeName are asked for because an LIC due list simply does not carry
// them — without SumAssured the client's portfolio would show a policy with zero cover.
type LinkImportedPolicyDTO struct {
	UserID         string  `json:"user_id" binding:"required" example:"64f1a2b3c4d5e6f7a8b9c0d1"`
	FamilyMemberID string  `json:"family_member_id" binding:"required"`
	SumAssured     float64 `json:"sum_assured" binding:"required,gt=0"`
	NomineeName    string  `json:"nominee_name,omitempty"`
	// PlanName is free text (e.g. "Jeevan Labh"); the PDF only carries the numeric plan code.
	PlanName string `json:"plan_name,omitempty"`
}

// ImportedPolicyRepository defines database operations for the imported-policy inbox.
type ImportedPolicyRepository interface {
	// BulkUpsertFromSync writes one row per policy number for the given agency, refreshing the
	// PDF-sourced fields of rows already present. Returns how many rows were newly created.
	BulkUpsertFromSync(ctx context.Context, records []*ImportedPolicy) (insertedCount int64, err error)
	// FindAll returns the agency's inbox, filtered by derived link status ("unclaimed"/"linked"/
	// "all") and an optional case-insensitive search across policy number and assured name.
	FindAll(ctx context.Context, agencyID, status, search string, page, limit int64) ([]*ImportedPolicyView, int64, error)
	FindByID(ctx context.Context, id bson.ObjectID) (*ImportedPolicy, error)
	Delete(ctx context.Context, id bson.ObjectID) error
	// CountByStatus returns (unclaimed, linked) totals for the agency — powers the inbox summary.
	CountByStatus(ctx context.Context, agencyID string) (unclaimed int64, linked int64, err error)
}
