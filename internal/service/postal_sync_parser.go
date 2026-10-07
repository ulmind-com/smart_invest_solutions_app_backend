package service

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PostalParsedRecord is one account read from a Post Office report ("POSTAL REPORT"): the agency's
// own list of the deposits it has opened for customers.
type PostalParsedRecord struct {
	AccountNo       string
	HolderName      string
	JointHolderName string
	// Scheme as printed ("5YR MIS", "1TD", "KVP", "NSC"…) and split into its parts.
	Scheme         string
	SchemeCode     string // MIS, TD, KVP, NSC, RD, SCSS…
	TermYears      int    // from the scheme label when it carries one ("5YR MIS" → 5); 0 otherwise
	DepositAmount  float64
	MaturityAmount float64
	// MonthlyIncome is set instead of MaturityAmount for schemes that pay out monthly, where the
	// report prints "585 P.M" in the maturity column.
	MonthlyIncome float64
	IssueDate     time.Time
	MaturityDate  time.Time
	Remarks       string
}

// Column labels of the report's repeated page header — skipped wherever they appear.
var postalHeaderLabels = map[string]bool{
	"account holder name": true,
	"joint holder name":   true,
	"account number":      true,
	"scheme":              true,
	"deposit amount":      true,
	"maturity amount":     true,
	"issue date":          true,
	"maturity date":       true,
	"remarks":             true,
}

var (
	// Post office account numbers in these reports are 10–12 digits and are the one field that
	// never repeats, so they anchor every record.
	postalAccountRegex = regexp.MustCompile(`^\d{10,12}$`)
	postalDateRegex    = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{4})$`)
	// Amounts print either plain (90000) or with Indian grouping (1,12,435).
	postalMoneyRegex = regexp.MustCompile(`^[\d,]+(?:\.\d+)?$`)
	// Monthly-income schemes print the payout instead of a maturity value: "585 P.M".
	postalMonthlyRegex = regexp.MustCompile(`^([\d,]+(?:\.\d+)?)\s*P\.?\s*M\.?$`)
	// A scheme label: KVP, NSC, 1TD, 5YR MIS, 3YR TD…
	postalSchemeRegex = regexp.MustCompile(`^(?:(\d{1,2})\s*(?:YR|YEAR)?\s*)?([A-Z]{2,6})$`)
	// Holder names are printed in capitals; remarks ("Delivered", "Scholarship Amount") are not,
	// which is what tells the two apart when both sit between two records.
	postalNameRegex = regexp.MustCompile(`^[A-Z][A-Z .'()\-/]*[A-Z.)]$`)
)

// PostalParseResult is everything a report yielded: the accounts read, the ones that could not be
// completed, and the ones the report itself listed twice (the sample report repeats a handful of
// accounts across pages — the first occurrence wins, and the repeat is reported rather than
// silently dropped or counted as a failure).
type PostalParseResult struct {
	Records           []PostalParsedRecord
	UnparsedAccounts  []string
	DuplicateAccounts []string
}

// parsePostalReport reads every deposit account out of a Post Office report.
//
// The PDF extracts one cell per line, in column order, and two of the columns are optional: a
// single-holder account has no joint-holder line, and the maturity column is blank for some
// accounts. So rather than assuming a fixed number of lines per record, this anchors on the account
// number — the only field that is always present and always unambiguous — then reads the names
// immediately above it and the remaining cells below it by shape (text / amount / date).
//
// It returns the records it could read, plus the account numbers it saw but could not make a
// complete record of, so the admin is told exactly what was skipped instead of silently losing rows.
func parsePostalReport(rawText string) PostalParseResult {
	lines := postalCells(rawText)

	anchors := make([]int, 0, 64)
	for i, line := range lines {
		if postalAccountRegex.MatchString(line) {
			anchors = append(anchors, i)
		}
	}

	result := PostalParseResult{
		Records:           make([]PostalParsedRecord, 0, len(anchors)),
		UnparsedAccounts:  make([]string, 0),
		DuplicateAccounts: make([]string, 0),
	}
	seen := make(map[string]bool, len(anchors))

	for n, at := range anchors {
		accountNo := lines[at]

		// Where this record's own cells stop: the names that belong to the next record.
		end := len(lines)
		if n+1 < len(anchors) {
			end = anchors[n+1]
			for end > at+1 && postalNameRegex.MatchString(lines[end-1]) {
				end--
			}
		}

		rec := PostalParsedRecord{AccountNo: accountNo}
		rec.HolderName, rec.JointHolderName = postalHolders(lines, at)
		postalFillCells(&rec, lines[at+1:end])

		if seen[accountNo] {
			result.DuplicateAccounts = append(result.DuplicateAccounts, accountNo)
			continue
		}

		// A row is only usable if it names the scheme, the money and when it started — anything
		// less would create a deposit record nobody could act on.
		if rec.Scheme == "" || rec.DepositAmount <= 0 || rec.IssueDate.IsZero() {
			result.UnparsedAccounts = append(result.UnparsedAccounts, accountNo)
			continue
		}

		seen[accountNo] = true
		result.Records = append(result.Records, rec)
	}

	return result
}

// postalCells splits the extracted text into trimmed, non-empty cells with the page headers removed.
func postalCells(rawText string) []string {
	raw := strings.Split(strings.ReplaceAll(rawText, "\r\n", "\n"), "\n")
	cells := make([]string, 0, len(raw))
	for _, line := range raw {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || postalHeaderLabels[strings.ToLower(trimmed)] {
			continue
		}
		cells = append(cells, trimmed)
	}
	return cells
}

// postalHolders reads the one or two capitalised name cells sitting directly above an account
// number. Anything else above them (a previous row's remark, a stray total) is left alone.
func postalHolders(lines []string, at int) (holder, joint string) {
	names := make([]string, 0, 2)
	for i := at - 1; i >= 0 && len(names) < 2; i-- {
		if !postalNameRegex.MatchString(lines[i]) {
			break
		}
		names = append([]string{lines[i]}, names...)
	}

	switch len(names) {
	case 0:
		return "", ""
	case 1:
		return names[0], ""
	default:
		return names[0], names[1]
	}
}

// postalFillCells reads a record's cells, which follow the account number in column order:
// scheme, deposit amount, maturity amount (or a monthly payout, or nothing), issue date,
// maturity date, remarks.
func postalFillCells(rec *PostalParsedRecord, cells []string) {
	var remarks []string

	for _, cell := range cells {
		switch {
		case postalDateRegex.MatchString(cell):
			if rec.IssueDate.IsZero() {
				rec.IssueDate = parsePostalDate(cell)
			} else if rec.MaturityDate.IsZero() {
				rec.MaturityDate = parsePostalDate(cell)
			}

		case postalMonthlyRegex.MatchString(cell):
			rec.MonthlyIncome = parsePostalAmount(postalMonthlyRegex.FindStringSubmatch(cell)[1])

		case postalMoneyRegex.MatchString(cell):
			if rec.DepositAmount == 0 {
				rec.DepositAmount = parsePostalAmount(cell)
			} else if rec.MaturityAmount == 0 {
				rec.MaturityAmount = parsePostalAmount(cell)
			}

		case rec.Scheme == "":
			rec.Scheme = strings.Join(strings.Fields(strings.ToUpper(cell)), " ")
			if m := postalSchemeRegex.FindStringSubmatch(rec.Scheme); m != nil {
				rec.SchemeCode = m[2]
				if years, err := strconv.Atoi(m[1]); err == nil {
					rec.TermYears = years
				}
			}

		default:
			remarks = append(remarks, cell)
		}
	}

	rec.Remarks = strings.Join(remarks, " ")
}

// parsePostalDate reads the report's dd/mm/yyyy dates. Deposits are compared and displayed as whole
// days, so they are stored at midnight UTC like every other date in the system.
func parsePostalDate(value string) time.Time {
	m := postalDateRegex.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return time.Time{}
	}
	day, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	year, _ := strconv.Atoi(m[3])
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

// parsePostalAmount reads an amount printed with or without Indian digit grouping.
func parsePostalAmount(value string) float64 {
	cleaned := strings.ReplaceAll(strings.TrimSpace(value), ",", "")
	amount, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0
	}
	return amount
}

// postalTermMonths is how long the deposit runs, taken from the dates on the report (the scheme
// label is unreliable: the sample report has "1YR TD" rows with a five-year maturity date). Falls
// back to the scheme's own term when the report carries no maturity date.
func postalTermMonths(rec PostalParsedRecord) int {
	if !rec.IssueDate.IsZero() && !rec.MaturityDate.IsZero() && rec.MaturityDate.After(rec.IssueDate) {
		// Rounded, not truncated: a one-year deposit is 365 days, which divides to 11.99 months.
		months := int(math.Round(rec.MaturityDate.Sub(rec.IssueDate).Hours() / 24 / 30.44))
		if months > 0 {
			return months
		}
	}
	if rec.TermYears > 0 {
		return rec.TermYears * 12
	}
	return 0
}

// postalAccountType is the human label stored on the deposit, e.g. "Post Office — MIS".
func postalAccountType(rec PostalParsedRecord) string {
	scheme := rec.SchemeCode
	if scheme == "" {
		scheme = rec.Scheme
	}
	if scheme == "" {
		return "Post Office"
	}
	return "Post Office — " + scheme
}
