package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Status constants for ReferralRecord
const (
	ReferralStatusPending   = "Pending"
	ReferralStatusCompleted = "Completed"
)

// ReferralRecord attributes one client signup to the staff member whose referral code it used.
// It is created Pending when the person applies and turns Completed once their account exists, so
// a super admin can see which admin brought in which clients — and how many.
type ReferralRecord struct {
	ID         bson.ObjectID `bson:"_id,omitempty" json:"id"`
	ReferrerID bson.ObjectID `bson:"referrer_id" json:"referrer_id"`
	// ReferredEmail is the applicant's email, normalised — the key the record is completed by.
	ReferredEmail string `bson:"referred_email" json:"referred_email"`
	ReferredName  string `bson:"referred_name,omitempty" json:"referred_name,omitempty"`
	ReferredPhone string `bson:"referred_phone,omitempty" json:"referred_phone,omitempty"`
	// ReferredUserID is stamped when the referral completes, i.e. once the client account exists.
	ReferredUserID *bson.ObjectID `bson:"referred_user_id,omitempty" json:"referred_user_id,omitempty"`
	Status         string         `bson:"status" json:"status"` // Pending, Completed
	CompletedAt    *time.Time     `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
	CreatedAt      time.Time      `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time      `bson:"updated_at" json:"updated_at"`
}

// ReferralStatsDTO is a staff member's own referral summary: the code they share and how their
// referrals are doing. Referrals carry no reward — they are attribution, not a loyalty scheme.
type ReferralStatsDTO struct {
	ReferralCode   string `json:"referral_code"`
	TotalPending   int64  `json:"total_pending"`
	TotalCompleted int64  `json:"total_completed"`
}

// ReferralRecordWithDetails is a ledger row enriched with who referred whom.
type ReferralRecordWithDetails struct {
	ID            bson.ObjectID `bson:"_id" json:"id"`
	ReferrerID    bson.ObjectID `bson:"referrer_id" json:"referrer_id"`
	ReferrerName  string        `bson:"referrer_name" json:"referrer_name"`
	ReferrerEmail string        `bson:"referrer_email" json:"referrer_email"`
	// ReferrerAdminID is the referring staff member's Admin ID (empty for legacy client referrals).
	ReferrerAdminID string         `bson:"referrer_admin_id,omitempty" json:"referrer_admin_id,omitempty"`
	ReferrerRole    string         `bson:"referrer_role,omitempty" json:"referrer_role,omitempty"`
	ReferredEmail   string         `bson:"referred_email" json:"referred_email"`
	ReferredName    string         `bson:"referred_name,omitempty" json:"referred_name,omitempty"`
	ReferredPhone   string         `bson:"referred_phone,omitempty" json:"referred_phone,omitempty"`
	ReferredUserID  *bson.ObjectID `bson:"referred_user_id,omitempty" json:"referred_user_id,omitempty"`
	Status          string         `bson:"status" json:"status"`
	CompletedAt     *time.Time     `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
	CreatedAt       time.Time      `bson:"created_at" json:"created_at"`
	UpdatedAt       time.Time      `bson:"updated_at" json:"updated_at"`
}

// ReferralListResponse is a paginated slice of the referral ledger.
type ReferralListResponse struct {
	Total int64                        `json:"total"`
	Data  []*ReferralRecordWithDetails `json:"data"`
}

// AdminReferralSummary is one row of the super admin's per-admin referral leaderboard.
type AdminReferralSummary struct {
	AdminUserID    bson.ObjectID `json:"admin_user_id"`
	AdminID        string        `json:"admin_id,omitempty"`
	Name           string        `json:"name"`
	Email          string        `json:"email"`
	ReferralCode   string        `json:"referral_code,omitempty"`
	Role           string        `json:"role"`
	TotalPending   int64         `json:"total_pending"`
	TotalCompleted int64         `json:"total_completed"`
	Total          int64         `json:"total"`
}

// ReferralCounts is the pending/completed split for a single referrer.
type ReferralCounts struct {
	Pending   int64
	Completed int64
}

// ReferralRepository defines database operations for referral records.
type ReferralRepository interface {
	Create(ctx context.Context, record *ReferralRecord) (*ReferralRecord, error)
	GetPendingByReferredEmail(ctx context.Context, email string) (*ReferralRecord, error)
	// Complete marks a pending referral as converted and stamps the account it produced.
	Complete(ctx context.Context, id bson.ObjectID, referredUserID bson.ObjectID, referredName, referredPhone string) error
	CountsByReferrerID(ctx context.Context, referrerID bson.ObjectID) (ReferralCounts, error)
	// CountsByReferrer returns the pending/completed split for every referrer in one pass.
	CountsByReferrer(ctx context.Context) (map[bson.ObjectID]ReferralCounts, error)
	// GetAll lists the ledger, newest first. referrerID, when set, narrows it to one referrer —
	// which is how a plain admin only ever sees their own referrals.
	GetAll(ctx context.Context, page, limit int64, referrerID *bson.ObjectID) ([]*ReferralRecordWithDetails, int64, error)
	// ReassignReferrer moves every referral made by fromUserID to toUserID (family account merge).
	ReassignReferrer(ctx context.Context, fromUserID, toUserID bson.ObjectID) (int64, error)
}

// ReferralService defines business logic operations for the referral scheme.
type ReferralService interface {
	// GetMyStats returns the calling staff member's own code and referral counts.
	GetMyStats(ctx context.Context, requesterRole, requesterID string) (*ReferralStatsDTO, error)
	// GetAllReferrals lists the ledger: a super admin sees every referral (optionally filtered to
	// one referrer); a plain admin only ever sees the referrals they made themselves.
	GetAllReferrals(ctx context.Context, requesterRole, requesterID, referrerIDFilter string, page, limit int64) (*ReferralListResponse, error)
	// GetAdminSummary returns the per-admin leaderboard. Super admin only.
	GetAdminSummary(ctx context.Context, requesterRole string) ([]*AdminReferralSummary, error)
}
