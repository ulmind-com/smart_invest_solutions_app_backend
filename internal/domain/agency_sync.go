package domain

import (
	"context"
	"time"
)

// SyncResultDTO represents the summary result returned after processing an LIC Premium Due List PDF.
type SyncResultDTO struct {
	TotalPoliciesFoundInPDF int              `json:"total_policies_found_in_pdf"`
	SuccessfullyUpdatedInDB int              `json:"successfully_updated_in_db"`
	FailedToUpdateInDB      int              `json:"failed_to_update_in_db"`
	UnmappedPolicies        []UnmappedPolicy `json:"unmapped_policies"`
}

// UnmappedPolicy represents a policy record extracted from the PDF that does not exist in the database.
type UnmappedPolicy struct {
	PolicyNo    string  `json:"policy_no"`
	AssuredName string  `json:"assured_name"`
	DOC         string  `json:"doc"`
	FUP         string  `json:"fup"`
	Mode        string  `json:"mode"`
	Premium     float64 `json:"premium"`
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
	ProcessLICDueList(ctx context.Context, fileBytes []byte) (*SyncResultDTO, error)
}
