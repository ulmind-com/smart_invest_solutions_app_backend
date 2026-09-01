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
)

type agencySyncService struct {
	lifeInsuranceRepo domain.LifeInsuranceRepository
}

// NewAgencySyncService initializes a new AgencySyncService.
func NewAgencySyncService(lifeInsuranceRepo domain.LifeInsuranceRepository) domain.AgencySyncService {
	return &agencySyncService{
		lifeInsuranceRepo: lifeInsuranceRepo,
	}
}

// ProcessLICDueList parses an uploaded LIC Premium Due List PDF, extracts policy records,
// calculates next due dates, reconciles with the database, and performs bulk updates.
func (s *agencySyncService) ProcessLICDueList(ctx context.Context, fileBytes []byte) (*domain.SyncResultDTO, error) {
	if len(fileBytes) == 0 {
		return nil, fmt.Errorf("uploaded file is empty")
	}

	// Step 1: Extract raw text from PDF bytes
	rawText, err := extractTextFromPDF(fileBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to extract text from PDF: %w", err)
	}

	if strings.TrimSpace(rawText) == "" {
		return nil, fmt.Errorf("unable to read text from PDF file or file is empty")
	}

	// Step 2: Extract policy records from raw text
	parsedRecords := parseLICRecordsFromText(rawText)
	if len(parsedRecords) == 0 {
		return &domain.SyncResultDTO{
			TotalPoliciesFoundInPDF: 0,
			SuccessfullyUpdatedInDB: 0,
			UnmappedPolicies:        []domain.UnmappedPolicy{},
		}, nil
	}

	// Step 3: Collect policy numbers for DB lookup
	policyNos := make([]string, 0, len(parsedRecords))
	for _, rec := range parsedRecords {
		policyNos = append(policyNos, rec.PolicyNo)
	}

	// Step 4: Reconcile policy numbers against MongoDB database
	existingMap, err := s.lifeInsuranceRepo.GetExistingPolicyNumbers(ctx, policyNos)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing policies from database: %w", err)
	}

	var matchedRecords []domain.LICParsedRecord
	var unmappedPolicies []domain.UnmappedPolicy

	for _, rec := range parsedRecords {
		if existingMap[rec.PolicyNo] {
			matchedRecords = append(matchedRecords, rec)
		} else {
			unmappedPolicies = append(unmappedPolicies, domain.UnmappedPolicy{
				PolicyNo:              rec.PolicyNo,
				AssuredName:           rec.AssuredName,
				DOC:                   rec.DOC,
				FUP:                   rec.FUP,
				Mode:                  rec.Mode,
				Premium:               rec.Premium,
				CalculatedNextDueDate: rec.CalculatedNextDueDate,
			})
		}
	}

	// Step 5: Perform bulk database updates for matched records
	var updatedCount int64
	var failedCount int
	if len(matchedRecords) > 0 {
		updatedCount, failedCount, err = s.lifeInsuranceRepo.BulkUpdateFromSync(ctx, matchedRecords)
		if err != nil {
			return nil, fmt.Errorf("failed to execute bulk update: %w", err)
		}
	}

	return &domain.SyncResultDTO{
		TotalPoliciesFoundInPDF: len(parsedRecords),
		SuccessfullyUpdatedInDB: int(updatedCount),
		FailedToUpdateInDB:      failedCount,
		UnmappedPolicies:        unmappedPolicies,
	}, nil
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

// parseLICRecordsFromText extracts structured LIC records using regex strategy & Date Math.
func parseLICRecordsFromText(rawText string) []domain.LICParsedRecord {
	// Normalize text: replace newlines, tabs, and vertical bars with spaces
	normalized := strings.ReplaceAll(rawText, "\r\n", " ")
	normalized = strings.ReplaceAll(normalized, "\n", " ")
	normalized = strings.ReplaceAll(normalized, "\r", " ")
	normalized = strings.ReplaceAll(normalized, "|", " ")
	normalized = strings.ReplaceAll(normalized, "\t", " ")

	spaceRegex := regexp.MustCompile(`\s+`)
	normalized = spaceRegex.ReplaceAllString(normalized, " ")

	recordMap := make(map[string]domain.LICParsedRecord)

	// Primary Pattern: Match complete LIC Due List rows
	// Matches: PolicyNo(9 digits) AssuredName DOC(DD/MM/YYYY) Mode(Yly|Hly|Qly|Mly) FUP(MM/YYYY) Premium(float)
	rowRegex := regexp.MustCompile(`(?i)\b(\d{9})\b\s+([A-Z\s\.\,\'-]{2,35}?)\s+(\d{2}/\d{2}/\d{4})\s+(Yly|Hly|Qly|Mly|Yearly|Half-Yearly|Quarterly|Monthly|Y|H|Q|M|SSS)\s+(\d{1,2}/\d{4})\s+(\d+(?:\.\d{1,2})?)`)
	matches := rowRegex.FindAllStringSubmatch(normalized, -1)

	for _, match := range matches {
		if len(match) >= 7 {
			policyNo := match[1]
			name := strings.TrimSpace(match[2])
			docStr := match[3]
			modeStr := match[4]
			fupStr := match[5]
			premiumStr := match[6]

			premium, _ := strconv.ParseFloat(premiumStr, 64)
			nextDueDate := calculateNextDueDate(docStr, fupStr)

			rec := domain.LICParsedRecord{
				PolicyNo:              policyNo,
				AssuredName:           cleanName(name),
				DOC:                   docStr,
				FUP:                   fupStr,
				Mode:                  normalizeMode(modeStr),
				Premium:               premium,
				CalculatedNextDueDate: nextDueDate,
			}
			recordMap[policyNo] = rec
		}
	}

	// Fallback Pattern: Find 9-digit policy numbers that were missed by primary row regex
	policyNoRegex := regexp.MustCompile(`\b\d{9}\b`)
	policyLocs := policyNoRegex.FindAllStringIndex(normalized, -1)

	docRegex := regexp.MustCompile(`\b(\d{2}/\d{2}/\d{4})\b`)
	modeRegex := regexp.MustCompile(`(?i)\b(Yly|Hly|Qly|Mly|Yearly|Half-Yearly|Quarterly|Monthly|SSS)\b`)
	fupRegex := regexp.MustCompile(`\b(\d{1,2}/\d{4})\b`)
	numRegex := regexp.MustCompile(`\b(\d{3,7}(?:\.\d{1,2})?)\b`)

	for _, loc := range policyLocs {
		policyNo := normalized[loc[0]:loc[1]]
		if _, exists := recordMap[policyNo]; exists {
			continue
		}

		// Look ahead in window of 180 characters
		endIdx := loc[1] + 180
		if endIdx > len(normalized) {
			endIdx = len(normalized)
		}
		window := normalized[loc[1]:endIdx]

		docMatch := docRegex.FindString(window)
		modeMatch := modeRegex.FindString(window)
		fupMatch := fupRegex.FindString(window)

		if docMatch != "" && fupMatch != "" {
			numMatches := numRegex.FindAllString(window, -1)
			var premium float64
			for _, n := range numMatches {
				p, err := strconv.ParseFloat(n, 64)
				if err == nil && p > 50 { // Valid premium threshold
					premium = p
					break
				}
			}

			// Extract name substring between policyNo and DOC
			name := "UNKNOWN"
			docIdx := strings.Index(window, docMatch)
			if docIdx > 0 {
				namePart := window[:docIdx]
				name = cleanName(namePart)
			}

			if modeMatch == "" {
				modeMatch = "Yly"
			}

			nextDueDate := calculateNextDueDate(docMatch, fupMatch)
			recordMap[policyNo] = domain.LICParsedRecord{
				PolicyNo:              policyNo,
				AssuredName:           name,
				DOC:                   docMatch,
				FUP:                   fupMatch,
				Mode:                  normalizeMode(modeMatch),
				Premium:               premium,
				CalculatedNextDueDate: nextDueDate,
			}
		}
	}

	records := make([]domain.LICParsedRecord, 0, len(recordMap))
	for _, rec := range recordMap {
		records = append(records, rec)
	}

	return records
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

func cleanName(raw string) string {
	name := strings.TrimSpace(raw)
	// Remove common PDF table headers or non-name noise
	cleanRegex := regexp.MustCompile(`[^A-Za-z\s\.\,\'-]`)
	name = cleanRegex.ReplaceAllString(name, "")
	name = regexp.MustCompile(`\s+`).ReplaceAllString(name, " ")
	name = strings.TrimSpace(name)

	if len(name) < 2 {
		return "VALUED CLIENT"
	}
	return name
}
