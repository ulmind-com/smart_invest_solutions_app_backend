package service

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/sync/errgroup"
)

// reportService is a pure orchestrator: it owns no collection of its own and holds no state
// beyond references to the repositories it pulls a client's portfolio data from.
type reportService struct {
	userRepo             domain.UserRepository
	familyMemberRepo     domain.FamilyMemberRepository
	lifeInsuranceRepo    domain.LifeInsuranceRepository
	healthInsuranceRepo  domain.HealthInsuranceRepository
	generalInsuranceRepo domain.GeneralInsuranceRepository
	fixedDepositRepo     domain.FixedDepositRepository
}

// NewReportService creates a new instance of ReportService.
func NewReportService(
	userRepo domain.UserRepository,
	familyMemberRepo domain.FamilyMemberRepository,
	lifeInsuranceRepo domain.LifeInsuranceRepository,
	healthInsuranceRepo domain.HealthInsuranceRepository,
	generalInsuranceRepo domain.GeneralInsuranceRepository,
	fixedDepositRepo domain.FixedDepositRepository,
) domain.ReportService {
	return &reportService{
		userRepo:             userRepo,
		familyMemberRepo:     familyMemberRepo,
		lifeInsuranceRepo:    lifeInsuranceRepo,
		healthInsuranceRepo:  healthInsuranceRepo,
		generalInsuranceRepo: generalInsuranceRepo,
		fixedDepositRepo:     fixedDepositRepo,
	}
}

// dateLayout is used to render every date column in the generated PDF.
const dateLayout = "02-Jan-2006"

// GenerateClientPortfolio fetches a client's profile, family members, and every policy/FD they
// hold (concurrently, since each read hits a different collection), then renders it all into a
// single in-memory PDF.
func (s *reportService) GenerateClientPortfolio(ctx context.Context, targetUserID string) ([]byte, error) {
	userID, err := bson.ObjectIDFromHex(targetUserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	var (
		user            *domain.User
		familyMembers   []*domain.FamilyMember
		lifePolicies    []*domain.LifeInsurance
		healthPolicies  []*domain.HealthInsurance
		generalPolicies []*domain.GeneralInsurance
		fixedDeposits   []*domain.FixedDeposit
	)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		u, err := s.userRepo.FindByID(gctx, userID)
		user = u
		return err
	})
	g.Go(func() error {
		members, _, err := s.familyMemberRepo.FindAllByUserID(gctx, userID)
		familyMembers = members
		return err
	})
	g.Go(func() error {
		policies, _, err := s.lifeInsuranceRepo.GetByUserID(gctx, userID)
		lifePolicies = policies
		return err
	})
	g.Go(func() error {
		policies, _, err := s.healthInsuranceRepo.GetByUserID(gctx, userID)
		healthPolicies = policies
		return err
	})
	g.Go(func() error {
		policies, _, err := s.generalInsuranceRepo.FindAllByUserID(gctx, userID)
		generalPolicies = policies
		return err
	})
	g.Go(func() error {
		fds, _, err := s.fixedDepositRepo.GetByUserID(gctx, userID)
		fixedDeposits = fds
		return err
	})

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("failed to load client portfolio data: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("client not found")
	}

	return renderPortfolioPDF(user, familyMembers, lifePolicies, healthPolicies, generalPolicies, fixedDeposits)
}

// renderPortfolioPDF builds the actual PDF document and returns its raw bytes.
func renderPortfolioPDF(
	user *domain.User,
	familyMembers []*domain.FamilyMember,
	lifePolicies []*domain.LifeInsurance,
	healthPolicies []*domain.HealthInsurance,
	generalPolicies []*domain.GeneralInsurance,
	fixedDeposits []*domain.FixedDeposit,
) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddPage()

	// Header
	pdf.SetFont("Helvetica", "B", 18)
	pdf.CellFormat(0, 10, "Smart Invest Solutions - Client Portfolio", "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(100, 100, 100)
	pdf.CellFormat(0, 6, "Generated on: "+time.Now().UTC().Format(dateLayout), "", 1, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(4)

	// Section 1: Client Details
	addSectionTitle(pdf, "Client Details")
	pdf.SetFont("Helvetica", "", 11)
	pdf.CellFormat(30, 7, "Name:", "", 0, "L", false, 0, "")
	pdf.CellFormat(0, 7, user.Name, "", 1, "L", false, 0, "")
	pdf.CellFormat(30, 7, "Phone:", "", 0, "L", false, 0, "")
	pdf.CellFormat(0, 7, user.Phone, "", 1, "L", false, 0, "")
	pdf.Ln(4)

	// Family Members
	if len(familyMembers) > 0 {
		addSectionTitle(pdf, "Family Members")
		rows := make([][]string, 0, len(familyMembers))
		for _, m := range familyMembers {
			rows = append(rows, []string{m.Name, m.RelationWithHOF, m.Phone, m.DateOfBirth})
		}
		addTable(pdf, []string{"Name", "Relation", "Phone", "Date of Birth"}, []float64{50, 40, 40, 50}, rows)
		pdf.Ln(4)
	}

	// Section 2: Life Insurance
	addSectionTitle(pdf, "Life Insurance")
	if len(lifePolicies) == 0 {
		addEmptyNote(pdf, "No life insurance policies on record.")
	} else {
		rows := make([][]string, 0, len(lifePolicies))
		for _, p := range lifePolicies {
			rows = append(rows, []string{
				p.PolicyDetails.PolicyNo,
				p.PolicyDetails.PlanName,
				formatAmount(p.PolicyDetails.SumAssured),
				p.PremiumDetails.NextDueDate.Format(dateLayout),
			})
		}
		addTable(pdf, []string{"Policy No", "Plan Name", "Sum Assured", "Next Due Date"}, []float64{40, 55, 40, 45}, rows)
	}
	pdf.Ln(4)

	// Section 3: Health Insurance
	addSectionTitle(pdf, "Health Insurance")
	if len(healthPolicies) == 0 {
		addEmptyNote(pdf, "No health insurance policies on record.")
	} else {
		rows := make([][]string, 0, len(healthPolicies))
		for _, p := range healthPolicies {
			rows = append(rows, []string{
				p.PolicyDetails.PolicyNo,
				p.PolicyDetails.PlanName,
				formatAmount(p.PolicyDetails.SumInsured),
				p.PremiumDetails.NextDueDate.Format(dateLayout),
			})
		}
		addTable(pdf, []string{"Policy No", "Plan Name", "Sum Insured", "Next Due Date"}, []float64{40, 55, 40, 45}, rows)
	}
	pdf.Ln(4)

	// Section 4: General Insurance
	addSectionTitle(pdf, "General Insurance")
	if len(generalPolicies) == 0 {
		addEmptyNote(pdf, "No general insurance policies on record.")
	} else {
		rows := make([][]string, 0, len(generalPolicies))
		for _, p := range generalPolicies {
			rows = append(rows, []string{p.PolicyNo, p.CompanyName, p.VehicleNo, p.DateOfExpiry})
		}
		addTable(pdf, []string{"Policy No", "Company Name", "Vehicle No", "Expiry Date"}, []float64{40, 55, 40, 45}, rows)
	}
	pdf.Ln(4)

	// Section 5: Fixed Deposits
	addSectionTitle(pdf, "Fixed Deposits")
	if len(fixedDeposits) == 0 {
		addEmptyNote(pdf, "No fixed deposits on record.")
	} else {
		rows := make([][]string, 0, len(fixedDeposits))
		for _, fd := range fixedDeposits {
			rows = append(rows, []string{
				fd.FDNumber,
				fd.FDName,
				formatAmount(fd.MaturityAmount),
				fd.MaturityDate.Format(dateLayout),
			})
		}
		addTable(pdf, []string{"FD No", "FD Name", "Maturity Amount", "Maturity Date"}, []float64{40, 55, 40, 45}, rows)
	}

	if pdf.Error() != nil {
		return nil, fmt.Errorf("failed to render PDF: %w", pdf.Error())
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("failed to write PDF output: %w", err)
	}

	return buf.Bytes(), nil
}

// addSectionTitle renders a bold section heading with a thin rule underneath.
func addSectionTitle(pdf *gofpdf.Fpdf, title string) {
	pdf.SetFont("Helvetica", "B", 13)
	pdf.CellFormat(0, 8, title, "B", 1, "L", false, 0, "")
	pdf.Ln(2)
}

// addEmptyNote renders a small italic placeholder line for a section with no data.
func addEmptyNote(pdf *gofpdf.Fpdf, note string) {
	pdf.SetFont("Helvetica", "I", 10)
	pdf.SetTextColor(120, 120, 120)
	pdf.CellFormat(0, 6, note, "", 1, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
}

// addTable renders a header row (shaded) followed by one row per entry, using fixed column widths.
func addTable(pdf *gofpdf.Fpdf, headers []string, colWidths []float64, rows [][]string) {
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetFillColor(230, 230, 230)
	for i, h := range headers {
		pdf.CellFormat(colWidths[i], 8, h, "1", 0, "L", true, 0, "")
	}
	pdf.Ln(-1)

	pdf.SetFont("Helvetica", "", 10)
	for _, row := range rows {
		for i, cell := range row {
			pdf.CellFormat(colWidths[i], 8, cell, "1", 0, "L", false, 0, "")
		}
		pdf.Ln(-1)
	}
}

// formatAmount renders a currency amount with thousands-separated rupee formatting kept simple —
// two decimal places, no locale-specific grouping (avoids pulling in a formatting dependency).
func formatAmount(amount float64) string {
	return fmt.Sprintf("Rs. %.2f", amount)
}
