package domain

import (
	"context"
	"time"
)

// SyncResultDTO represents the summary result returned after processing an LIC Premium Due List PDF.
type SyncResultDTO struct {
	TotalPoliciesFoundInPDF int `json:"total_policies_found_in_pdf"`
	SuccessfullyUpdatedInDB int `json:"successfully_updated_in_db"`
	FailedToUpdateInDB      int `json:"failed_to_update_in_db"`
	// FailedPolicies names exactly which policy numbers failed to update and why, so an admin
	// isn't left with just a bare failure count and no way to diagnose or target a re-run.
	FailedPolicies []FailedSyncPolicy `json:"failed_policies"`
	// UnparsedPolicyNumbers lists 9-digit numbers the parser located in the PDF but could not fully
	// read (missing a DOC or FUP date nearby) — previously dropped with zero trace, so a row could
	// silently vanish from the sync with no signal to the admin that anything was even there.
	UnparsedPolicyNumbers []string         `json:"unparsed_policy_numbers"`
	UnmappedPolicies      []UnmappedPolicy `json:"unmapped_policies"`
}

// FailedSyncPolicy identifies one policy number whose bulk-update write failed, and the reason
// MongoDB reported for that specific record.
type FailedSyncPolicy struct {
	PolicyNo string `json:"policy_no"`
	Reason   string `json:"reason"`
}

// UnmappedPolicy represents a policy record extracted from the PDF that does not exist in the database.
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

// LICParsedRecord represents an internal policy record parsed from the PDF with calculated dates.
type LICParsedRecord struct {
	PolicyNo              string    `bson:"policy_no" json:"policy_no"`
	AssuredName           string    `bson:"assured_name" json:"assured_name"`
	DOC                   string    `bson:"doc" json:"doc"`
	FUP                   string    `bson:"fup" json:"fup"`
	Mode                  string    `bson:"mode" json:"mode"`
	Premium               float64   `bson:"premium" json:"premium"`
	CalculatedNextDueDate time.Time `bson:"calculated_next_due_date" json:"calculated_next_due_date"`
}

// AgencySyncService defines business logic operations for agency PDF sync engines.
type AgencySyncService interface {
	ProcessLICDueList(ctx context.Context, requesterRole, requesterID string, fileBytes []byte) (*SyncResultDTO, error)
}
