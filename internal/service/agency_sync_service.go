package service

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// licCompanyName is stamped on every policy created from an LIC due list — the file format is
// LIC's own, so the insurer is never in question.
const licCompanyName = "Life Insurance Corporation of India"

type agencySyncService struct {
	lifeInsuranceRepo  domain.LifeInsuranceRepository
	importedPolicyRepo domain.ImportedPolicyRepository
	familyMemberRepo   domain.FamilyMemberRepository
	userRepo           domain.UserRepository
}

// NewAgencySyncService initializes a new AgencySyncService.
func NewAgencySyncService(
	lifeInsuranceRepo domain.LifeInsuranceRepository,
	importedPolicyRepo domain.ImportedPolicyRepository,
	familyMemberRepo domain.FamilyMemberRepository,
	userRepo domain.UserRepository,
) domain.AgencySyncService {
	return &agencySyncService{
		lifeInsuranceRepo:  lifeInsuranceRepo,
		importedPolicyRepo: importedPolicyRepo,
		familyMemberRepo:   familyMemberRepo,
		userRepo:           userRepo,
	}
}

// resolveSyncAgency returns the calling admin's agency, failing closed. Agency Sync is admin-only
// (super_admin is rejected in the handler), so an admin whose agency can't be resolved must never
// fall through to an unscoped sync that could touch another agency's policies.
func (s *agencySyncService) resolveSyncAgency(ctx context.Context, requesterRole, requesterID string) (string, error) {
	agencyID := resolveCallerAgencyID(ctx, s.userRepo, requesterRole, requesterID)
	if agencyID == "" {
		return "", fmt.Errorf("unable to resolve your agency — contact support")
	}
	return agencyID, nil
}

// ProcessLICDueList parses an uploaded LIC Premium Due List PDF and reconciles it with the app:
//
//  1. every row is stored in the agency's policy inbox (imported_policies), so the agency's whole
//     book lives in the app — including policies whose owner has no app account yet;
//  2. rows whose policy number already belongs to a client of this agency have their premium,
//     payment mode, next due date and DOC refreshed from the PDF.
//
// Everything is scoped to the calling admin's own agency, so a policy number that happens to
// coincide with another agency's client is never read or written.
func (s *agencySyncService) ProcessLICDueList(ctx context.Context, requesterRole, requesterID string, fileBytes []byte) (*domain.SyncResultDTO, error) {
	if len(fileBytes) == 0 {
		return nil, fmt.Errorf("uploaded file is empty")
	}

	agencyID, err := s.resolveSyncAgency(ctx, requesterRole, requesterID)
	if err != nil {
		return nil, err
	}

	rawText, err := extractTextFromPDF(fileBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to extract text from PDF: %w", err)
	}
	if strings.TrimSpace(rawText) == "" {
		return nil, fmt.Errorf("unable to read text from PDF file or file is empty")
	}

	header, parsedRecords, unparsedPolicyNos := parseLICDueList(rawText)
	if unparsedPolicyNos == nil {
		unparsedPolicyNos = []string{}
	}

	result := &domain.SyncResultDTO{
		DueMonth:                header.DueMonth,
		AgentCode:               header.AgentCode,
		AgentName:               header.AgentName,
		TotalPoliciesFoundInPDF: len(parsedRecords),
		UnparsedPolicyNumbers:   unparsedPolicyNos,
		UnmappedPolicies:        []domain.UnmappedPolicy{},
		FailedPolicies:          []domain.FailedSyncPolicy{},
	}

	if len(parsedRecords) == 0 {
		// Nothing readable in the file. Still report the running inbox size so the screen isn't blank.
		if unclaimed, _, err := s.importedPolicyRepo.CountByStatus(ctx, agencyID); err == nil {
			result.UnclaimedTotal = int(unclaimed)
		}
		return result, nil
	}

	// Step 1: persist every row into the agency's policy inbox (idempotent on agency+policy number).
	inbox := make([]*domain.ImportedPolicy, 0, len(parsedRecords))
	for _, rec := range parsedRecords {
		inbox = append(inbox, &domain.ImportedPolicy{
			AgencyID:            agencyID,
			PolicyNo:            rec.PolicyNo,
			AssuredName:         rec.AssuredName,
			DOC:                 parseDueListDate(rec.DOC),
			PlanCode:            rec.PlanCode,
			Term:                rec.Term,
			Mode:                rec.Mode,
			FUP:                 rec.FUP,
			Flag:                rec.Flag,
			InstallmentPremium:  rec.Premium,
			DueCount:            rec.DueCount,
			TotalPremium:        rec.TotalPremium,
			EstimatedCommission: rec.EstimatedCommission,
			NextDueDate:         rec.CalculatedNextDueDate,
			AgentCode:           header.AgentCode,
			DueMonth:            header.DueMonth,
		})
	}

	newlyImported, err := s.importedPolicyRepo.BulkUpsertFromSync(ctx, inbox)
	if err != nil {
		return nil, fmt.Errorf("failed to store imported policies: %w", err)
	}
	result.NewlyImported = int(newlyImported)

	// Step 2: refresh the policies that already belong to a client of this agency.
	policyNos := make([]string, 0, len(parsedRecords))
	for _, rec := range parsedRecords {
		policyNos = append(policyNos, rec.PolicyNo)
	}

	existingMap, err := s.lifeInsuranceRepo.GetExistingPolicyNumbers(ctx, policyNos, agencyID)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing policies from database: %w", err)
	}

	var matchedRecords []domain.LICParsedRecord
	for _, rec := range parsedRecords {
		if existingMap[rec.PolicyNo] {
			matchedRecords = append(matchedRecords, rec)
			continue
		}
		result.UnmappedPolicies = append(result.UnmappedPolicies, domain.UnmappedPolicy{
			PolicyNo:              rec.PolicyNo,
			AssuredName:           rec.AssuredName,
			DOC:                   rec.DOC,
			FUP:                   rec.FUP,
			Mode:                  rec.Mode,
			Premium:               rec.Premium,
			CalculatedNextDueDate: rec.CalculatedNextDueDate,
		})
	}

	if len(matchedRecords) > 0 {
		updatedCount, failedPolicies, err := s.lifeInsuranceRepo.BulkUpdateFromSync(ctx, matchedRecords, agencyID)
		if err != nil {
			return nil, fmt.Errorf("failed to execute bulk update: %w", err)
		}
		result.SuccessfullyUpdatedInDB = int(updatedCount)
		if failedPolicies != nil {
			result.FailedPolicies = failedPolicies
		}
		result.FailedToUpdateInDB = len(result.FailedPolicies)
	}

	// Step 3: the running follow-up figure — how much of the book still has no client account.
	if unclaimed, _, err := s.importedPolicyRepo.CountByStatus(ctx, agencyID); err == nil {
		result.UnclaimedTotal = int(unclaimed)
	}

	return result, nil
}

// ListImportedPolicies returns the calling admin's policy inbox with live link status.
func (s *agencySyncService) ListImportedPolicies(ctx context.Context, requesterRole, requesterID, status, search string, page, limit int64) ([]*domain.ImportedPolicyView, int64, error) {
	agencyID, err := s.resolveSyncAgency(ctx, requesterRole, requesterID)
	if err != nil {
		return nil, 0, err
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	switch status {
	case domain.ImportedPolicyStatusUnclaimed, domain.ImportedPolicyStatusLinked, domain.ImportedPolicyStatusAll:
	default:
		status = domain.ImportedPolicyStatusAll
	}

	return s.importedPolicyRepo.FindAll(ctx, agencyID, status, strings.TrimSpace(search), page, limit)
}

// LinkImportedPolicy attaches an unclaimed inbox row to a real client account by creating the Life
// Insurance record for it. The policy number carries over untouched — it is the identity that lets
// every later due-list upload keep this policy's premium and due date current automatically.
//
// Field ownership after linking: the sync owns the money and the dates (premium, payment mode,
// next due date, DOC) and refreshes them monthly; the admin owns sum assured, nominee, plan name,
// term and PPT, and the sync never overwrites those.
func (s *agencySyncService) LinkImportedPolicy(ctx context.Context, requesterRole, requesterID, idStr string, dto *domain.LinkImportedPolicyDTO) (*domain.LifeInsurance, error) {
	agencyID, err := s.resolveSyncAgency(ctx, requesterRole, requesterID)
	if err != nil {
		return nil, err
	}

	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid imported policy ID format: %w", err)
	}

	imported, err := s.importedPolicyRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	// Same "not found" wording as a genuinely missing row, so another agency's inbox can't be probed.
	if imported.AgencyID != agencyID {
		return nil, fmt.Errorf("imported policy not found")
	}

	userID, err := bson.ObjectIDFromHex(dto.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid client ID format: %w", err)
	}

	targetUser, err := s.userRepo.FindByID(ctx, userID)
	if err != nil || targetUser == nil {
		return nil, fmt.Errorf("client account not found")
	}
	if !canAccessAgencyScopedRecord(requesterRole, agencyID, targetUser.AgencyID) {
		return nil, fmt.Errorf("client account not found")
	}

	familyMemberID, err := bson.ObjectIDFromHex(dto.FamilyMemberID)
	if err != nil {
		return nil, fmt.Errorf("invalid family member ID format: %w", err)
	}

	familyMember, err := s.familyMemberRepo.FindByID(ctx, familyMemberID)
	if err != nil || familyMember == nil {
		return nil, fmt.Errorf("family member not found")
	}
	if familyMember.UserID != userID {
		return nil, fmt.Errorf("family member does not belong to the selected client")
	}

	// An LIC due list has no sum assured column, so it is collected at link time rather than
	// letting a policy reach the client's portfolio showing zero cover.
	if dto.SumAssured <= 0 {
		return nil, fmt.Errorf("sum assured is required to link a policy")
	}

	term := imported.Term
	if term < 1 {
		term = 1
	}

	planName := strings.TrimSpace(dto.PlanName)
	if planName == "" && imported.PlanCode != "" {
		planName = fmt.Sprintf("LIC Plan %s", imported.PlanCode)
	}
	if planName == "" {
		planName = "LIC Policy"
	}

	policy := &domain.LifeInsurance{
		UserID:         userID,
		FamilyMemberID: familyMemberID,
		CompanyName:    licCompanyName,
		PolicyDetails: domain.PolicyDetails{
			PolicyNo: imported.PolicyNo,
			PlanName: planName,
			// The insured's name is cached from the family member the admin picked, matching how
			// every other Life policy in the app is created.
			LifeInsuredName: familyMember.Name,
			NomineeName:     strings.TrimSpace(dto.NomineeName),
			SumAssured:      dto.SumAssured,
			Term:            term,
			// The due list carries no separate premium-paying term; defaulting PPT to the policy
			// term covers the common case and the admin can correct it on the policy screen.
			PPT:          term,
			DOC:          imported.DOC,
			MaturityDate: imported.DOC.AddDate(term, 0, 0),
		},
		PremiumDetails: domain.PremiumDetails{
			InstallmentPremium: imported.InstallmentPremium,
			NextDueDate:        imported.NextDueDate,
			PaymentMode:        imported.Mode,
		},
		IsMapped: true,
	}

	created, err := s.lifeInsuranceRepo.Create(ctx, policy)
	if err != nil {
		return nil, err
	}

	return created, nil
}

// DeleteImportedPolicy removes an inbox row — used when the wrong file was uploaded. A row that is
// already linked to a client's policy is refused, so this can never be mistaken for a way to
// delete the client's actual policy.
func (s *agencySyncService) DeleteImportedPolicy(ctx context.Context, requesterRole, requesterID, idStr string) error {
	agencyID, err := s.resolveSyncAgency(ctx, requesterRole, requesterID)
	if err != nil {
		return err
	}

	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return fmt.Errorf("invalid imported policy ID format: %w", err)
	}

	imported, err := s.importedPolicyRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if imported.AgencyID != agencyID {
		return fmt.Errorf("imported policy not found")
	}

	existing, err := s.lifeInsuranceRepo.GetExistingPolicyNumbers(ctx, []string{imported.PolicyNo}, agencyID)
	if err != nil {
		return fmt.Errorf("failed to check whether this policy is linked: %w", err)
	}
	if existing[imported.PolicyNo] {
		return fmt.Errorf("this policy is linked to a client account — remove it from that client's policies first")
	}

	return s.importedPolicyRepo.Delete(ctx, id)
}

// extractTextFromPDF reads all text content from an in-memory PDF byte slice using ledongthuc/pdf.
func extractTextFromPDF(fileBytes []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(fileBytes), int64(len(fileBytes)))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	numPages := r.NumPage()

	for pageIndex := 1; pageIndex <= numPages; pageIndex++ {
		p := r.Page(pageIndex)
		if p.V.IsNull() {
			continue
		}

		text, err := p.GetPlainText(nil)
		if err == nil && strings.TrimSpace(text) != "" {
			buf.WriteString(text)
			buf.WriteString("\n")
		} else {
			// Fallback to Content.Text objects if GetPlainText returns empty
			content := p.Content()
			for _, textObj := range content.Text {
				buf.WriteString(textObj.S)
				buf.WriteString(" ")
			}
			buf.WriteString("\n")
		}
	}

	return buf.String(), nil
}

// licRowRegex matches one complete row of an LIC Premium Due List, in the exact column order the
// report prints them:
//
//	S.No | PolicyNo | Name of Assured | D.o.C | Pln/Tm | Mod | FUP | Flg | InstPrem | Due | GST | TotPrem | EstCom
//
// Anchoring on the *whole* row rather than a few landmarks is what makes the amounts trustworthy:
// a looser pattern happily reads the year out of the D.o.C or the plan number out of "Pln/Tm" and
// writes it into a client's premium. Every column is therefore matched in sequence, and anything
// that doesn't fit the shape is reported as unreadable instead of being guessed at.
var licRowRegex = regexp.MustCompile(
	`\b(\d{9})\s+` + // 1  policy number
		`([A-Z][A-Z .,'()\-]*?)\s+` + // 2  name of assured
		`(\d{2}/\d{2}/\d{4})\s+` + // 3  date of commencement
		`(\d{1,3})/(\d{1,3})\s+` + // 4  plan code, 5 term (the "Pln/Tm" column)
		`(?i:(Yly|Hly|Qly|Mly|SSS))\s+` + // 6  mode
		`(\d{1,2}/\d{4})\s+` + // 7  first unpaid premium (MM/YYYY)
		`(?:(FY|ST|MT|LP)\s+)?` + // 8  flag (absent on renewal rows)
		`(\d+(?:\.\d{1,2})?)\s+` + // 9  installment premium
		`(\d+)\s+` + // 10 number of instalments due
		`(\d+(?:\.\d{1,2})?)\s+` + // 11 GST
		`(\d+(?:\.\d{1,2})?)\s+` + // 12 total premium
		`(\d+(?:\.\d{1,2})?)\b`, // 13 estimated commission
)

var (
	licPolicyNoRegex  = regexp.MustCompile(`\b\d{9}\b`)
	licAgentCodeRegex = regexp.MustCompile(`(?i)Agent\s*Code\s*:?\s*([A-Z]{2,4}\d{6,12})`)
	licAgentNameRegex = regexp.MustCompile(`(?i)Agent\s*Name\s*:?\s*([A-Z][A-Za-z .]*?)\s+(?:Agent|Branch|Premium)\b`)
	licBranchRegex    = regexp.MustCompile(`(?i)Branch\s*Code\s*:?\s*(\d{1,6})`)
	licDueMonthRegex  = regexp.MustCompile(`(?i)(?:Due\s*(?:Month)?\s*:?\s*|For\s+)(\d{1,2}/\d{4})`)
	licWhitespace     = regexp.MustCompile(`\s+`)
)

// parseLICDueList extracts the report header and every readable policy row from the PDF's text.
// The third return value lists 9-digit policy numbers that appear in the file but could not be
// read as a complete row — they are surfaced to the admin rather than silently dropped or, worse,
// reconstructed from guessed values.
func parseLICDueList(rawText string) (domain.LICDueListHeader, []domain.LICParsedRecord, []string) {
	normalized := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ", "|", " ").Replace(rawText)
	normalized = licWhitespace.ReplaceAllString(normalized, " ")

	header := domain.LICDueListHeader{}
	if m := licAgentCodeRegex.FindStringSubmatch(normalized); len(m) > 1 {
		header.AgentCode = strings.ToUpper(strings.TrimSpace(m[1]))
	}
	if m := licAgentNameRegex.FindStringSubmatch(normalized); len(m) > 1 {
		header.AgentName = strings.TrimSpace(m[1])
	}
	if m := licBranchRegex.FindStringSubmatch(normalized); len(m) > 1 {
		header.BranchCode = strings.TrimSpace(m[1])
	}
	if m := licDueMonthRegex.FindStringSubmatch(normalized); len(m) > 1 {
		header.DueMonth = strings.TrimSpace(m[1])
	}

	// A due list can legitimately repeat a policy number across pages; the map keeps the last read
	// of each so one policy contributes exactly one record.
	recordMap := make(map[string]domain.LICParsedRecord)

	for _, match := range licRowRegex.FindAllStringSubmatch(normalized, -1) {
		policyNo := match[1]
		term, _ := strconv.Atoi(match[5])
		premium, _ := strconv.ParseFloat(match[9], 64)
		dueCount, _ := strconv.Atoi(match[10])
		totalPremium, _ := strconv.ParseFloat(match[12], 64)
		estCommission, _ := strconv.ParseFloat(match[13], 64)

		recordMap[policyNo] = domain.LICParsedRecord{
			PolicyNo:              policyNo,
			AssuredName:           cleanName(match[2]),
			DOC:                   match[3],
			PlanCode:              match[4],
			Term:                  term,
			Mode:                  normalizeMode(match[6]),
			FUP:                   match[7],
			Flag:                  strings.ToUpper(match[8]),
			Premium:               premium,
			DueCount:              dueCount,
			TotalPremium:          totalPremium,
			EstimatedCommission:   estCommission,
			CalculatedNextDueDate: calculateNextDueDate(match[3], match[7]),
		}
	}

	// Anything shaped like a policy number that no complete row claimed is reported as unreadable.
	// Guessing values for these is what previously corrupted premiums, so they are never inferred.
	seenUnparsed := make(map[string]bool)
	var unparsedPolicyNos []string
	for _, policyNo := range licPolicyNoRegex.FindAllString(normalized, -1) {
		if _, parsed := recordMap[policyNo]; parsed || seenUnparsed[policyNo] {
			continue
		}
		seenUnparsed[policyNo] = true
		unparsedPolicyNos = append(unparsedPolicyNos, policyNo)
	}

	records := make([]domain.LICParsedRecord, 0, len(recordMap))
	for _, rec := range recordMap {
		records = append(records, rec)
	}

	return header, records, unparsedPolicyNos
}

// parseDueListDate converts a DD/MM/YYYY date as printed in the due list into a UTC time.
func parseDueListDate(value string) time.Time {
	parsed, err := time.Parse("02/01/2006", strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

// calculateNextDueDate implements the required Date Math logic:
// Extract Day from DOC (DD/MM/YYYY). Extract Month & Year from FUP (MM/YYYY).
// Combine Year, Month, Day to form CalculatedNextDueDate (UTC).
func calculateNextDueDate(docStr, fupStr string) time.Time {
	// Parse DOC for Day
	docParts := strings.Split(docStr, "/")
	docDay := 1
	if len(docParts) == 3 {
		if d, err := strconv.Atoi(docParts[0]); err == nil && d >= 1 && d <= 31 {
			docDay = d
		}
	}

	// Parse FUP for Month and Year
	fupParts := strings.Split(fupStr, "/")
	fupMonth := time.Now().Month()
	fupYear := time.Now().Year()

	if len(fupParts) == 2 {
		if m, err := strconv.Atoi(fupParts[0]); err == nil && m >= 1 && m <= 12 {
			fupMonth = time.Month(m)
		}
		if y, err := strconv.Atoi(fupParts[1]); err == nil && y >= 1900 {
			fupYear = y
		}
	}

	// Calculate maximum days in target month (handles leap years & 28/29/30/31 boundaries)
	firstOfNextMonth := time.Date(fupYear, fupMonth+1, 1, 0, 0, 0, 0, time.UTC)
	lastDayOfMonth := firstOfNextMonth.AddDate(0, 0, -1).Day()

	targetDay := docDay
	if targetDay > lastDayOfMonth {
		targetDay = lastDayOfMonth
	}
	if targetDay < 1 {
		targetDay = 1
	}

	return time.Date(fupYear, fupMonth, targetDay, 0, 0, 0, 0, time.UTC)
}

func normalizeMode(mode string) string {
	switch strings.TrimSpace(strings.ToUpper(mode)) {
	case "YLY", "Y", "YEARLY":
		return domain.PaymentModeYearly
	case "HLY", "H", "HALF-YEARLY", "HALF YEARLY":
		return domain.PaymentModeHalfYearly
	case "QLY", "Q", "QUARTERLY":
		return domain.PaymentModeQuarterly
	case "MLY", "M", "MONTHLY", "SSS":
		return domain.PaymentModeMonthly
	default:
		if mode != "" {
			return mode
		}
		return domain.PaymentModeYearly
	}
}

// cleanName tidies the captured assured name without editorialising it: the row regex already
// constrains what can be captured, so this only collapses whitespace. Suffixes the report itself
// prints — "(LA)", "-LA", "NM" — are left intact, because they are how the agent recognises the
// record in LIC's own paperwork.
func cleanName(raw string) string {
	name := licWhitespace.ReplaceAllString(strings.TrimSpace(raw), " ")
	name = strings.Trim(name, " .,-")
	if len(name) < 2 {
		return "VALUED CLIENT"
	}
	return name
}
