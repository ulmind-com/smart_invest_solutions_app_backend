package service

import (
	"context"
	"math"

	"github.com/smart-invest-solutions/backend/internal/domain"
)

type calculatorService struct {
	repo domain.CalculatorSettingsRepository
}

// NewCalculatorService initializes a new CalculatorService.
func NewCalculatorService(repo domain.CalculatorSettingsRepository) domain.CalculatorService {
	return &calculatorService{
		repo: repo,
	}
}

// GetSettings retrieves the current global calculator settings.
func (s *calculatorService) GetSettings(ctx context.Context) (*domain.CalculatorSettings, error) {
	return s.repo.GetSettings(ctx)
}

// UpdateSettings modifies global calculator default return rates.
func (s *calculatorService) UpdateSettings(ctx context.Context, dto *domain.UpdateCalculatorSettingsDTO) (*domain.CalculatorSettings, error) {
	current, err := s.repo.GetSettings(ctx)
	if err != nil {
		return nil, err
	}

	if dto.DefaultSIPRate != nil {
		current.DefaultSIPRate = *dto.DefaultSIPRate
	}
	if dto.DefaultLumpsumRate != nil {
		current.DefaultLumpsumRate = *dto.DefaultLumpsumRate
	}
	if dto.DefaultFDRate != nil {
		current.DefaultFDRate = *dto.DefaultFDRate
	}

	return s.repo.UpsertSettings(ctx, current)
}

// CalculateSIP calculates Systematic Investment Plan returns using formula:
// M = P * [((1 + i)^n - 1) / i] * (1 + i)
func (s *calculatorService) CalculateSIP(ctx context.Context, req *domain.SIPRequestDTO) (*domain.CalculatorResponseDTO, error) {
	rate := 0.0
	if req.ExpectedReturnRate != nil && *req.ExpectedReturnRate > 0 {
		rate = *req.ExpectedReturnRate
	} else {
		settings, err := s.repo.GetSettings(ctx)
		if err != nil {
			return nil, err
		}
		rate = settings.DefaultSIPRate
	}

	p := req.MonthlyInvestment
	i := (rate / 100.0) / 12.0
	n := float64(req.TimePeriodYears * 12)

	var totalValue float64
	if i > 0 {
		totalValue = p * ((math.Pow(1+i, n) - 1) / i) * (1 + i)
	} else {
		totalValue = p * n
	}

	investedAmount := p * n
	estimatedReturns := totalValue - investedAmount

	return &domain.CalculatorResponseDTO{
		InvestedAmount:      roundToTwoDecimals(investedAmount),
		EstimatedReturns:    roundToTwoDecimals(estimatedReturns),
		TotalValue:          roundToTwoDecimals(totalValue),
		AppliedInterestRate: roundToTwoDecimals(rate),
	}, nil
}

// CalculateLumpsum calculates single investment compound interest returns using formula:
// A = P * (1 + r)^t
func (s *calculatorService) CalculateLumpsum(ctx context.Context, req *domain.LumpsumRequestDTO) (*domain.CalculatorResponseDTO, error) {
	rate := 0.0
	if req.ExpectedReturnRate != nil && *req.ExpectedReturnRate > 0 {
		rate = *req.ExpectedReturnRate
	} else {
		settings, err := s.repo.GetSettings(ctx)
		if err != nil {
			return nil, err
		}
		rate = settings.DefaultLumpsumRate
	}

	p := req.TotalInvestment
	r := rate / 100.0
	t := float64(req.TimePeriodYears)

	totalValue := p * math.Pow(1+r, t)
	investedAmount := p
	estimatedReturns := totalValue - investedAmount

	return &domain.CalculatorResponseDTO{
		InvestedAmount:      roundToTwoDecimals(investedAmount),
		EstimatedReturns:    roundToTwoDecimals(estimatedReturns),
		TotalValue:          roundToTwoDecimals(totalValue),
		AppliedInterestRate: roundToTwoDecimals(rate),
	}, nil
}

// CalculateFD calculates Fixed Deposit maturity value using compound interest formula:
// A = P * (1 + r/n)^(n * t)
func (s *calculatorService) CalculateFD(ctx context.Context, req *domain.FDRequestDTO) (*domain.CalculatorResponseDTO, error) {
	rate := 0.0
	if req.InterestRate != nil && *req.InterestRate > 0 {
		rate = *req.InterestRate
	} else {
		settings, err := s.repo.GetSettings(ctx)
		if err != nil {
			return nil, err
		}
		rate = settings.DefaultFDRate
	}

	// Determine compounding periods per year (n)
	var n float64
	switch req.CompoundingFrequency {
	case domain.CompoundingFrequencyQuarterly:
		n = 4.0
	case domain.CompoundingFrequencyHalfYearly:
		n = 2.0
	case domain.CompoundingFrequencyYearly:
		n = 1.0
	default:
		n = 4.0
	}

	p := req.Principal
	r := rate / 100.0
	t := float64(req.TenureMonths) / 12.0

	totalValue := p * math.Pow(1+(r/n), n*t)
	investedAmount := p
	estimatedReturns := totalValue - investedAmount

	return &domain.CalculatorResponseDTO{
		InvestedAmount:      roundToTwoDecimals(investedAmount),
		EstimatedReturns:    roundToTwoDecimals(estimatedReturns),
		TotalValue:          roundToTwoDecimals(totalValue),
		AppliedInterestRate: roundToTwoDecimals(rate),
	}, nil
}

func roundToTwoDecimals(val float64) float64 {
	return math.Round(val*100) / 100
}
