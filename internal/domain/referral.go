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

// ReferralRecord tracks a referral lead created when a prospective client applies a referral code.
type ReferralRecord struct {
	ID                 bson.ObjectID `bson:"_id,omitempty" json:"id"`
	ReferrerID         bson.ObjectID `bson:"referrer_id" json:"referrer_id"`
	ReferredEmail      string        `bson:"referred_email" json:"referred_email"`
	Status             string        `bson:"status" json:"status"` // Pending, Completed
	RewardDaysCredited int           `bson:"reward_days_credited" json:"reward_days_credited"`
	CreatedAt          time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt          time.Time     `bson:"updated_at" json:"updated_at"`
}

// ReferralStatsDTO represents the client-facing referral dashboard statistics.
type ReferralStatsDTO struct {
	ReferralCode       string    `json:"referral_code"`
	AppValidityEndDate time.Time `json:"app_validity_end_date"`
	TotalPending       int64     `json:"total_pending"`
	TotalCompleted     int64     `json:"total_completed"`
	TotalDaysEarned    int64     `json:"total_days_earned"`
}

// ReferralRecordWithDetails represents a referral record enriched with Referrer user details for Admin view.
type ReferralRecordWithDetails struct {
	ID                 bson.ObjectID `bson:"_id" json:"id"`
	ReferrerID         bson.ObjectID `bson:"referrer_id" json:"referrer_id"`
	ReferrerName       string        `bson:"referrer_name" json:"referrer_name"`
	ReferrerEmail      string        `bson:"referrer_email" json:"referrer_email"`
	ReferredEmail      string        `bson:"referred_email" json:"referred_email"`
	Status             string        `bson:"status" json:"status"`
	RewardDaysCredited int           `bson:"reward_days_credited" json:"reward_days_credited"`
	CreatedAt          time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt          time.Time     `bson:"updated_at" json:"updated_at"`
}

// ReferralListResponse represents a paginated response of referral records for Admin.
type ReferralListResponse struct {
	Total int64                        `json:"total"`
	Data  []*ReferralRecordWithDetails `json:"data"`
}

// ReferralRepository defines database operations for referral records.
type ReferralRepository interface {
	Create(ctx context.Context, record *ReferralRecord) (*ReferralRecord, error)
	GetPendingByReferredEmail(ctx context.Context, email string) (*ReferralRecord, error)
	UpdateStatus(ctx context.Context, id bson.ObjectID, status string, rewardDays int) error
	GetByReferrerID(ctx context.Context, referrerID bson.ObjectID) ([]*ReferralRecord, error)
	GetStatsByReferrerID(ctx context.Context, referrerID bson.ObjectID) (totalPending int64, totalCompleted int64, totalDays int64, err error)
	GetAll(ctx context.Context, page, limit int64) ([]*ReferralRecordWithDetails, int64, error)
}

// ReferralService defines business logic operations for the referral scheme.
type ReferralService interface {
	GetMyStats(ctx context.Context, userIDStr string) (*ReferralStatsDTO, error)
	GetAllReferrals(ctx context.Context, page, limit int64) (*ReferralListResponse, error)
}
