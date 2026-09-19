package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
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
// single in-memory PDF. A plain admin may only generate this for a client belonging to their own
// agency (same resolveCallerAgencyID/canAccessAgencyScopedRecord pattern used everywhere else) —
// this is checked up front, before any of the concurrent reads fire, so a cross-agency request
// never touches the target's financial data at all.
func (s *reportService) GenerateClientPortfolio(ctx context.Context, requesterRole, requesterID, targetUserID string) ([]byte, error) {
	userID, err := bson.ObjectIDFromHex(targetUserID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format: %w", err)
	}

	if requesterRole == domain.RoleAdmin {
		targetUser, err := s.userRepo.FindByID(ctx, userID)
		if err != nil || targetUser == nil {
			return nil, fmt.Errorf("user not found")
		}
		agencyFilter := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
		if !canAccessAgencyScopedRecord(requesterRole, agencyFilter, targetUser.AgencyID) {
			return nil, fmt.Errorf("user not found")
		}
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
	// The core Helvetica font is cp1252-encoded: translate UTF-8 text so accented characters render
	// correctly instead of as mojibake (characters outside cp1252 degrade to a placeholder).
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 18)
	pdf.CellFormat(0, 10, "Smart Invest Solutions - Client Portfolio", "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(100, 100, 100)
	pdf.CellFormat(0, 6, "Generated on: "+time.Now().In(indiaTZ).Format(dateLayout), "", 1, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.Ln(4)

	addSectionTitle(pdf, "Client Details")
	pdf.SetFont("Helvetica", "", 11)
	pdf.CellFormat(30, 7, "Name:", "", 0, "L", false, 0, "")
	pdf.CellFormat(0, 7, tr(user.Name), "", 1, "L", false, 0, "")
	pdf.CellFormat(30, 7, "Email:", "", 0, "L", false, 0, "")
	pdf.CellFormat(0, 7, tr(user.Email), "", 1, "L", false, 0, "")
	pdf.CellFormat(30, 7, "Phone:", "", 0, "L", false, 0, "")
	pdf.CellFormat(0, 7, tr(orDash(user.Phone)), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	memberNames := make(map[string]string, len(familyMembers))
	addSectionTitle(pdf, "Family Members")
	if len(familyMembers) == 0 {
		addEmptyNote(pdf, "No family members on record.")
	} else {
		rows := make([][]string, 0, len(familyMembers))
		for _, m := range familyMembers {
			memberNames[m.ID.Hex()] = m.Name
			rows = append(rows, []string{m.Name, m.RelationWithHOF, orDash(m.Phone), formatISODate(m.DateOfBirth), orDash(m.BloodGroup)})
		}
		addTable(pdf, tr, []string{"Name", "Relation", "Phone", "Date of Birth", "Blood Group"}, []float64{50, 35, 40, 30, 25}, rows)
	}
	pdf.Ln(4)

	insured := func(cached, familyMemberID string) string {
		if cached != "" {
			return cached
		}
		return orDash(memberNames[familyMemberID])
	}

	addSectionTitle(pdf, "Life Insurance")
	if len(lifePolicies) == 0 {
		addEmptyNote(pdf, "No life insurance policies on record.")
	} else {
		rows := make([][]string, 0, len(lifePolicies))
		for _, p := range lifePolicies {
			rows = append(rows, []string{
				p.PolicyDetails.PolicyNo,
				p.PolicyDetails.PlanName,
				insured(p.PolicyDetails.LifeInsuredName, p.FamilyMemberID.Hex()),
				orDash(p.PolicyDetails.NomineeName),
				formatAmount(p.PolicyDetails.SumAssured),
				formatAmount(p.PremiumDetails.InstallmentPremium) + " " + modeShort(p.PremiumDetails.PaymentMode),
				formatDate(p.PremiumDetails.NextDueDate),
				formatDate(p.PolicyDetails.MaturityDate),
			})
		}
		addTable(pdf, tr,
			[]string{"Policy No", "Plan", "Insured", "Nominee", "Sum Assured", "Premium", "Next Due", "Maturity"},
			[]float64{21, 26, 22, 22, 24, 25, 20, 20}, rows)
	}
	pdf.Ln(4)

	addSectionTitle(pdf, "Health Insurance")
	if len(healthPolicies) == 0 {
		addEmptyNote(pdf, "No health insurance policies on record.")
	} else {
		rows := make([][]string, 0, len(healthPolicies))
		for _, p := range healthPolicies {
			rows = append(rows, []string{
				p.PolicyDetails.PolicyNo,
				p.CompanyName + " - " + p.PolicyDetails.PlanName,
				insured(p.PolicyDetails.PrimaryInsuredName, p.FamilyMemberID.Hex()),
				formatAmount(p.PolicyDetails.SumInsured),
				formatAmount(p.PremiumDetails.InstallmentPremium) + " " + modeShort(p.PremiumDetails.PaymentMode),
				formatDate(p.PolicyDetails.DOC) + " to " + formatDate(p.PolicyDetails.ExpiryDate),
			})
		}
		addTable(pdf, tr,
			[]string{"Policy No", "Insurer / Plan", "Insured", "Sum Insured", "Premium", "Cover Period"},
			[]float64{24, 38, 26, 26, 26, 40}, rows)
	}
	pdf.Ln(4)

	addSectionTitle(pdf, "Motor Insurance")
	if len(generalPolicies) == 0 {
		addEmptyNote(pdf, "No motor insurance policies on record.")
	} else {
		rows := make([][]string, 0, len(generalPolicies))
		for _, p := range generalPolicies {
			rows = append(rows, []string{p.PolicyNo, p.CompanyName, p.VehicleNo, orDash(p.AdvisorName), formatISODate(p.DateOfExpiry)})
		}
		addTable(pdf, tr, []string{"Policy No", "Insurer", "Vehicle No", "Advisor", "Expiry Date"}, []float64{35, 45, 30, 40, 30}, rows)
	}
	pdf.Ln(4)

	addSectionTitle(pdf, "Fixed Deposits")
	if len(fixedDeposits) == 0 {
		addEmptyNote(pdf, "No fixed deposits on record.")
	} else {
		rows := make([][]string, 0, len(fixedDeposits))
		for _, fd := range fixedDeposits {
			holders := insured("", fd.FamilyMemberID.Hex())
			if fd.SecondHolderName != "" {
				holders += " & " + fd.SecondHolderName
			}
			rows = append(rows, []string{
				fd.FDNumber,
				fd.FDName + " (" + fd.CompanyName + ")",
				holders,
				formatAmount(fd.PrincipalAmount),
				formatAmount(fd.MaturityAmount),
				formatDate(fd.MaturityDate),
			})
		}
		addTable(pdf, tr,
			[]string{"FD No", "Deposit", "Holder(s)", "Principal", "Maturity Value", "Matures On"},
			[]float64{24, 40, 34, 27, 30, 25}, rows)
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

// addSectionTitle renders a bold section heading with an underline rule.
func addSectionTitle(pdf *gofpdf.Fpdf, title string) {
	pdf.SetFont("Helvetica", "B", 13)
	pdf.CellFormat(0, 8, title, "B", 1, "L", false, 0, "")
	pdf.Ln(2)
}

// addEmptyNote renders an italic placeholder line for a section with no records.
func addEmptyNote(pdf *gofpdf.Fpdf, note string) {
	pdf.SetFont("Helvetica", "I", 10)
	pdf.SetTextColor(120, 120, 120)
	pdf.CellFormat(0, 6, note, "", 1, "L", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
}

// addTable renders a bordered table. Cell text is translated for the core font and truncated with
// an ellipsis to fit its column, so a long plan or bank name never spills over its neighbours.
func addTable(pdf *gofpdf.Fpdf, tr func(string) string, headers []string, colWidths []float64, rows [][]string) {
	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetFillColor(230, 230, 230)
	for i, h := range headers {
		pdf.CellFormat(colWidths[i], 7, fitText(pdf, h, colWidths[i]), "1", 0, "L", true, 0, "")
	}
	pdf.Ln(-1)

	pdf.SetFont("Helvetica", "", 8)
	for _, row := range rows {
		for i, cell := range row {
			pdf.CellFormat(colWidths[i], 7, fitText(pdf, tr(cell), colWidths[i]), "1", 0, "L", false, 0, "")
		}
		pdf.Ln(-1)
	}
}

// fitText truncates text (with "...") so it fits a cell of the given width, allowing for padding.
func fitText(pdf *gofpdf.Fpdf, text string, width float64) string {
	available := width - 2*pdf.GetCellMargin()
	if pdf.GetStringWidth(text) <= available {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 && pdf.GetStringWidth(string(runes)+"...") > available {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}

// formatAmount renders rupees with Indian digit grouping, e.g. "Rs. 12,34,567".
func formatAmount(amount float64) string {
	rounded := int64(amount + 0.5)
	if amount < 0 {
		rounded = int64(amount - 0.5)
	}
	sign := ""
	if rounded < 0 {
		sign = "-"
		rounded = -rounded
	}
	digits := fmt.Sprintf("%d", rounded)
	if len(digits) > 3 {
		head, tail := digits[:len(digits)-3], digits[len(digits)-3:]
		var groups []string
		for len(head) > 2 {
			groups = append([]string{head[len(head)-2:]}, groups...)
			head = head[:len(head)-2]
		}
		if head != "" {
			groups = append([]string{head}, groups...)
		}
		digits = strings.Join(groups, ",") + "," + tail
	}
	return "Rs. " + sign + digits
}

// formatDate renders a stored timestamp as an Indian calendar date, or "-" when unset.
func formatDate(t time.Time) string {
	if t.IsZero() || t.Year() < 1900 {
		return "-"
	}
	return t.In(indiaTZ).Format(dateLayout)
}

// formatISODate renders a YYYY-MM-DD string (motor expiry, date of birth) in the report's layout.
func formatISODate(value string) string {
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(value)); err == nil {
		return t.Format(dateLayout)
	}
	return orDash(value)
}

// modeShort abbreviates a payment mode for a narrow table column.
func modeShort(mode string) string {
	switch mode {
	case domain.PaymentModeYearly:
		return "/yr"
	case domain.PaymentModeHalfYearly:
		return "/half-yr"
	case domain.PaymentModeQuarterly:
		return "/qtr"
	case domain.PaymentModeMonthly:
		return "/mo"
	}
	return ""
}

func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
