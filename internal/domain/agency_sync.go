package domain

import (
	"context"
	"time"
)

// SyncResultDTO represents the summary result returned after processing an LIC Premium Due List PDF.
type SyncResultDTO struct {
	// DueMonth / AgentCode / AgentName are read from the PDF header so the admin can confirm at a
	// glance that they uploaded the file they meant to (right agent code, right month).
	DueMonth  string `json:"due_month,omitempty"`
	AgentCode string `json:"agent_code,omitempty"`
	AgentName string `json:"agent_name,omitempty"`

	TotalPoliciesFoundInPDF int `json:"total_policies_found_in_pdf"`
	SuccessfullyUpdatedInDB int `json:"successfully_updated_in_db"`
	FailedToUpdateInDB      int `json:"failed_to_update_in_db"`
	// AlreadyCurrent counts matched policies whose schedule the due list would not move forward —
	// usually a re-upload of the same (or an older) month. They are not failures: without this
	// number a second upload looks like a broken sync, because nothing gets "updated".
	AlreadyCurrent int `json:"already_current"`
	// NewlyImported counts rows that had never been seen in any previous upload — they are now
	// sitting in the agency's policy inbox waiting to be linked to a client account.
	NewlyImported int `json:"newly_imported"`
	// UnclaimedTotal is the agency's *running* count of inbox rows with no client account yet —
	// not just from this upload, so it reflects the real size of the follow-up job.
	UnclaimedTotal int `json:"unclaimed_total"`
	// FailedPolicies names exactly which policy numbers failed to update and why, so an admin
	// isn't left with just a bare failure count and no way to diagnose or target a re-run.
	FailedPolicies []FailedSyncPolicy `json:"failed_policies"`
	// UnparsedPolicyNumbers lists 9-digit numbers the parser located in the PDF but could not read
	// as a complete row — so a row can never silently vanish from the sync with no signal that
	// anything was even there.
	UnparsedPolicyNumbers []string         `json:"unparsed_policy_numbers"`
	UnmappedPolicies      []UnmappedPolicy `json:"unmapped_policies"`
}

// FailedSyncPolicy identifies one policy number whose bulk-update write failed, and the reason
// MongoDB reported for that specific record.
type FailedSyncPolicy struct {
	PolicyNo string `json:"policy_no"`
	Reason   string `json:"reason"`
}

// UnmappedPolicy represents a policy record extracted from the PDF that has no matching policy
// under a client account yet. Every one of these is also persisted in the agency's policy inbox
// (see ImportedPolicy) — this list is just the subset that came from *this* upload.
type UnmappedPolicy struct {
	PolicyNo    string  `json:"policy_no"`
	AssuredName string  `json:"assured_name"`
	DOC         string  `json:"doc"`
	FUP         string  `json:"fup"`
	Mode        string  `json:"mode"`
	Premium     float64 `json:"premium"`
	// CalculatedNextDueDate is what the sync worked out from FUP and mode. It is
	// the point of the whole run, and an admin adding this policy by hand needs
	// it just as much as one whose policy already matched.
	CalculatedNextDueDate time.Time `json:"calculated_next_due_date"`
}

// LICParsedRecord is one fully-read row of an LIC Premium Due List, mirroring the PDF's columns:
// PolicyNo | Name of Assured | D.o.C | Pln/Tm | Mod | FUP | Flg | InstPrem | Due | GST | TotPrem | EstCom
type LICParsedRecord struct {
	PolicyNo    string `bson:"policy_no" json:"policy_no"`
	AssuredName string `bson:"assured_name" json:"assured_name"`
	DOC         string `bson:"doc" json:"doc"`
	// PlanCode and Term are the two halves of the "Pln/Tm" column (e.g. "714/18").
	PlanCode              string    `bson:"plan_code" json:"plan_code"`
	Term                  int       `bson:"term" json:"term"`
	FUP                   string    `bson:"fup" json:"fup"`
	Mode                  string    `bson:"mode" json:"mode"`
	Flag                  string    `bson:"flag" json:"flag"`
	Premium               float64   `bson:"premium" json:"premium"`
	DueCount              int       `bson:"due_count" json:"due_count"`
	TotalPremium          float64   `bson:"total_premium" json:"total_premium"`
	EstimatedCommission   float64   `bson:"estimated_commission" json:"estimated_commission"`
	CalculatedNextDueDate time.Time `bson:"calculated_next_due_date" json:"calculated_next_due_date"`
}

// LICDueListHeader holds the identifying fields printed at the top of a Premium Due List.
type LICDueListHeader struct {
	AgentCode  string
	AgentName  string
	BranchCode string
	DueMonth   string
}

// AgencySyncService defines business logic operations for agency PDF sync engines.
type AgencySyncService interface {
	ProcessLICDueList(ctx context.Context, requesterRole, requesterID string, fileBytes []byte) (*SyncResultDTO, error)
	// ListImportedPolicies returns the calling admin's policy inbox — every row ever read from
	// their due lists, with live link status.
	ListImportedPolicies(ctx context.Context, requesterRole, requesterID, status, search string, page, limit int64) ([]*ImportedPolicyView, int64, error)
	// LinkImportedPolicy attaches an unclaimed inbox row to a real client account, creating the
	// Life Insurance policy record from the PDF data plus the sum assured/nominee the admin supplies.
	LinkImportedPolicy(ctx context.Context, requesterRole, requesterID, idStr string, dto *LinkImportedPolicyDTO) (*LifeInsurance, error)
	// DeleteImportedPolicy removes an inbox row (e.g. the wrong file was uploaded). Only rows that
	// are not linked to a client policy can be removed.
	DeleteImportedPolicy(ctx context.Context, requesterRole, requesterID, idStr string) error
}
