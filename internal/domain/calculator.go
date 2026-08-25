package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Compounding frequency constants for FD calculations.
const (
	CompoundingFrequencyQuarterly  = "Quarterly"
	CompoundingFrequencyHalfYearly = "Half-Yearly"
	CompoundingFrequencyYearly     = "Yearly"
)

// CalculatorSettings represents global default return rates manipulated/configured by Admin.
type CalculatorSettings struct {
	ID                 bson.ObjectID `bson:"_id,omitempty" json:"id"`
	DefaultSIPRate     float64       `bson:"default_sip_rate" json:"default_sip_rate"`         // e.g. 12.0 (%)
	DefaultLumpsumRate float64       `bson:"default_lumpsum_rate" json:"default_lumpsum_rate"` // e.g. 12.0 (%)
	DefaultFDRate      float64       `bson:"default_fd_rate" json:"default_fd_rate"`           // e.g. 7.0 (%)
	UpdatedAt          time.Time     `bson:"updated_at" json:"updated_at"`
}

// UpdateCalculatorSettingsDTO represents payload sent by Admin to update global default rates.
type UpdateCalculatorSettingsDTO struct {
	DefaultSIPRate     *float64 `json:"default_sip_rate,omitempty" binding:"omitempty,gte=0"`
	DefaultLumpsumRate *float64 `json:"default_lumpsum_rate,omitempty" binding:"omitempty,gte=0"`
	DefaultFDRate      *float64 `json:"default_fd_rate,omitempty" binding:"omitempty,gte=0"`
}

// SIPRequestDTO represents request parameters for SIP calculation.
type SIPRequestDTO struct {
	MonthlyInvestment  float64  `json:"monthly_investment" binding:"required,gt=0"`
	ExpectedReturnRate *float64 `json:"expected_return_rate,omitempty"` // Optional: Uses Admin default if nil
	TimePeriodYears    int      `json:"time_period_years" binding:"required,gt=0"`
}

// LumpsumRequestDTO represents request parameters for Lumpsum calculation.
type LumpsumRequestDTO struct {
	TotalInvestment    float64  `json:"total_investment" binding:"required,gt=0"`
	ExpectedReturnRate *float64 `json:"expected_return_rate,omitempty"` // Optional: Uses Admin default if nil
	TimePeriodYears    int      `json:"time_period_years" binding:"required,gt=0"`
}

// FDRequestDTO represents request parameters for Fixed Deposit calculation.
type FDRequestDTO struct {
	Principal            float64  `json:"principal" binding:"required,gt=0"`
	InterestRate         *float64 `json:"interest_rate,omitempty"` // Optional: Uses Admin default if nil
	TenureMonths         int      `json:"tenure_months" binding:"required,gt=0"`
	CompoundingFrequency string   `json:"compounding_frequency" binding:"required,oneof=Quarterly Half-Yearly Yearly"`
}

// CalculatorResponseDTO represents response data for any financial calculation.
type CalculatorResponseDTO struct {
	InvestedAmount      float64 `json:"invested_amount"`
	EstimatedReturns    float64 `json:"estimated_returns"`
	TotalValue          float64 `json:"total_value"`
	AppliedInterestRate float64 `json:"applied_interest_rate"` // Tells client which rate was applied
}

// CalculatorSettingsRepository defines database operations for global calculator settings.
type CalculatorSettingsRepository interface {
	GetSettings(ctx context.Context) (*CalculatorSettings, error)
	UpsertSettings(ctx context.Context, settings *CalculatorSettings) (*CalculatorSettings, error)
}

// CalculatorService defines business logic operations for financial calculations and settings management.
type CalculatorService interface {
	GetSettings(ctx context.Context) (*CalculatorSettings, error)
	UpdateSettings(ctx context.Context, dto *UpdateCalculatorSettingsDTO) (*CalculatorSettings, error)
	CalculateSIP(ctx context.Context, req *SIPRequestDTO) (*CalculatorResponseDTO, error)
	CalculateLumpsum(ctx context.Context, req *LumpsumRequestDTO) (*CalculatorResponseDTO, error)
	CalculateFD(ctx context.Context, req *FDRequestDTO) (*CalculatorResponseDTO, error)
}
