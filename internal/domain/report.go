package domain

import "context"

// ReportService defines business logic operations for generating downloadable client reports.
// It owns no collection of its own — it purely orchestrates reads across the existing user,
// family member, and financial-entity repositories to render a PDF in-memory.
type ReportService interface {
	// GenerateClientPortfolio renders a full portfolio PDF (client details, family members, and a
	// table per financial module: Life/Health/General Insurance and Fixed Deposits) for the given
	// target user, returning the raw PDF bytes.
	GenerateClientPortfolio(ctx context.Context, targetUserID string) ([]byte, error)
}
