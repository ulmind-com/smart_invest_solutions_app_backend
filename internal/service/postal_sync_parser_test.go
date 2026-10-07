package service

import (
	"strings"
	"testing"
	"time"
)

// postalReportText mirrors exactly how a real POSTAL REPORT PDF extracts: one cell per line in
// column order, the page header repeated on every page, and the awkward shapes that make a
// fixed-line-count parser wrong — a single-holder account (no joint holder cell), a monthly-income
// account whose maturity column reads "585 P.M", an amount with Indian digit grouping, a row with
// no maturity amount at all, a remark sitting between two records, and an account the report lists
// twice. Names and account numbers are invented; the shape is the real one.
const postalReportText = `
 Account Holder Name
 Joint Holder Name
 Account Number
 Scheme
 Deposit Amount
 Maturity Amount
 Issue Date
 Maturity Date
 Remarks
 ANIL KUMAR SAHA
 MITALI SAHA
 20165161217
 5YR MIS
 90000
 585 P.M
 01/07/2025
 01/07/2030
 BIKASH PAL
 RUMA PAL
 20165164495
 1YR TD
 70000
 74956
 01/07/2025
 01/07/2026
 CHANDAN DEY
 KABITA DEY
 20165329705
 1TD
 105000
 1,12,435
 02/07/2025
 02/07/2026
 Delivered
 DIPAK MONDAL
 20171638904
 KVP
 20000
 40000
 29/08/2025
 29/03/2035

 Account Holder Name
 Joint Holder Name
 Account Number
 Scheme
 Deposit Amount
 Maturity Amount
 Issue Date
 Maturity Date
 Remarks
 ESHA ROY
 PARTHA ROY
 20181558043
 1TD
 100000
 07/11/2025
 07/11/2026
 FARIDA BEGUM
 20193847169
 NSC
 20000
 28981
 17/01/2026
 17/01/2031
 Scholarship Amount
 ANIL KUMAR SAHA
 MITALI SAHA
 20165161217
 5YR MIS
 90000
 585 P.M
 01/07/2025
 01/07/2030
`

func findPostal(t *testing.T, records []PostalParsedRecord, accountNo string) PostalParsedRecord {
	t.Helper()
	for _, r := range records {
		if r.AccountNo == accountNo {
			return r
		}
	}
	t.Fatalf("account %s was not parsed", accountNo)
	return PostalParsedRecord{}
}

func TestParsePostalReport(t *testing.T) {
	res := parsePostalReport(postalReportText)

	if len(res.Records) != 6 {
		t.Fatalf("expected 6 accounts, got %d", len(res.Records))
	}
	if len(res.UnparsedAccounts) != 0 {
		t.Fatalf("nothing should be unreadable, got %v", res.UnparsedAccounts)
	}
	// The repeated account is reported as a duplicate, not as a failure, and is kept only once.
	if len(res.DuplicateAccounts) != 1 || res.DuplicateAccounts[0] != "20165161217" {
		t.Fatalf("expected one duplicate account, got %v", res.DuplicateAccounts)
	}

	// Monthly-income account: the maturity column carries the payout, not a maturity value.
	mis := findPostal(t, res.Records, "20165161217")
	if mis.HolderName != "ANIL KUMAR SAHA" || mis.JointHolderName != "MITALI SAHA" {
		t.Fatalf("holders read wrong: %+v", mis)
	}
	if mis.SchemeCode != "MIS" || mis.TermYears != 5 {
		t.Fatalf("scheme read wrong: %+v", mis)
	}
	if mis.DepositAmount != 90000 || mis.MonthlyIncome != 585 || mis.MaturityAmount != 0 {
		t.Fatalf("money read wrong: %+v", mis)
	}
	if got := postalTermMonths(mis); got != 60 {
		t.Fatalf("a five-year deposit is 60 months, got %d", got)
	}
	if !mis.IssueDate.Equal(time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)) ||
		!mis.MaturityDate.Equal(time.Date(2030, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("dates read wrong: %+v", mis)
	}
	// A monthly-income scheme returns the principal at maturity.
	if got := maturityValueFor(mis); got != 90000 {
		t.Fatalf("maturity value for a monthly-income deposit should be the principal, got %v", got)
	}

	// Indian digit grouping in the maturity column.
	grouped := findPostal(t, res.Records, "20165329705")
	if grouped.MaturityAmount != 112435 {
		t.Fatalf("1,12,435 should read as 112435, got %v", grouped.MaturityAmount)
	}
	if grouped.SchemeCode != "TD" || grouped.TermYears != 1 {
		t.Fatalf("\"1TD\" should read as a one-year TD: %+v", grouped)
	}

	// Single-holder account, directly after another record's remark.
	single := findPostal(t, res.Records, "20171638904")
	if single.HolderName != "DIPAK MONDAL" || single.JointHolderName != "" {
		t.Fatalf("a single-holder account must not borrow a name: %+v", single)
	}
	if single.SchemeCode != "KVP" || single.DepositAmount != 20000 || single.MaturityAmount != 40000 {
		t.Fatalf("single-holder row read wrong: %+v", single)
	}

	// Remarks belong to the record above them, never to the next one's holder name.
	withRemark := findPostal(t, res.Records, "20165329705")
	if withRemark.Remarks != "Delivered" {
		t.Fatalf("expected the remark on the record above it, got %q", withRemark.Remarks)
	}
	if nsc := findPostal(t, res.Records, "20193847169"); nsc.Remarks != "Scholarship Amount" {
		t.Fatalf("expected the trailing remark, got %q", nsc.Remarks)
	}

	// No maturity amount printed: the row is still usable, with the figure left unset.
	noMaturity := findPostal(t, res.Records, "20181558043")
	if noMaturity.DepositAmount != 100000 || noMaturity.MaturityAmount != 0 {
		t.Fatalf("a blank maturity column must not shift the deposit amount: %+v", noMaturity)
	}
	if !noMaturity.MaturityDate.Equal(time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("maturity date read wrong: %+v", noMaturity)
	}

	if got := postalAccountType(single); got != "Post Office — KVP" {
		t.Fatalf("account type label wrong: %q", got)
	}
}

func TestParsePostalReportIgnoresNonPostalText(t *testing.T) {
	// The LIC due list — fed to the wrong parser — yields nothing rather than nonsense rows.
	res := parsePostalReport(realDueListText)
	if len(res.Records) != 0 {
		t.Fatalf("an LIC due list is not a postal report; got %d records", len(res.Records))
	}
}

func TestParsePostalReportSkipsIncompleteRows(t *testing.T) {
	text := strings.Join([]string{
		" Account Holder Name",
		" GOPAL SEN",
		" 20199999999", // account number with nothing usable after it
		" ",
	}, "\n")

	res := parsePostalReport(text)
	if len(res.Records) != 0 {
		t.Fatalf("an incomplete row must not become a record: %+v", res.Records)
	}
	if len(res.UnparsedAccounts) != 1 || res.UnparsedAccounts[0] != "20199999999" {
		t.Fatalf("an incomplete row must be reported, got %v", res.UnparsedAccounts)
	}
}
