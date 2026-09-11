package domain

import "context"

// ReportService defines business logic operations for generating downloadable client reports.
// It owns no collection of its own — it purely orchestrates reads across the existing user,
// family member, and financial-entity repositories to render a PDF in-memory.
type ReportService interface {
	// GenerateClientPortfolio renders a full portfolio PDF (client details, family members, and a
	// table per financial module: Life/Health/General Insurance and Fixed Deposits) for the given
	// target user, returning the raw PDF bytes. A super_admin may target any client; a plain admin
	// only a client belonging to their own agency (requesterRole/requesterID identify the caller
	// for that check) — a client requester always gets their own report regardless of targetUserID.
	GenerateClientPortfolio(ctx context.Context, requesterRole, requesterID, targetUserID string) ([]byte, error)
}
