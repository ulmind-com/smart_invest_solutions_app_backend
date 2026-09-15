package service

import (
	"testing"
	"time"
)

// realDueListText mirrors the text an LIC Premium Due List PDF extracts to, using real rows from an
// actual report (header, renewal and first-year rows, odd names, page footer, legend). The columns
// are: S.No | PolicyNo | Name of Assured | D.o.C | Pln/Tm | Mod | FUP | Flg | InstPrem | Due | GST |
// TotPrem | EstCom.
const realDueListText = `Branch Code: 468
Agent Name : BIREN CHANDRA DATTA
Agent Code : LIC02579468
Premium Due List For The Agent LIC02579468 For 08/2026
S.No PolicyNo Name of Assured D.o.C Pln/Tm Mod FUP Flg InstPrem Due GST TotPrem EstCom
1 530766131 ATANU GHOSAL 06/08/2025 714/18 Hly 02/2026 FY 34493.00 2 0.00 68986 9485.57
3 479512074 SANJIB GORAI 24/02/2023 914/21 Hly 02/2026 2944.00 2 0.00 5888 294.40
10 530779905 SARFARAJ SEIKH(LA) 15/09/2025 734/20 Hly 03/2026 FY 6400.00 1 0.00 6400 1280.00
33 448135468 BIMAN GHOSH SO-SUBHASH GHOSH 10/04/2011 165/20 Hly 04/2026 3032.00 1 0.00 3032 151.60
48 466874490 RAHIMA BIBI 24/11/2006 179/20 Hly 05/2026 MT 1182.00 1 0.00 1182 59.10
57 464804871 CHANDRA MUKHERJEE 10/03/2003 14/25 Qly 06/2026 1131.00 1 0.00 1131 56.55
85 509608968 PAPIYA DUTTA MONDAL 28/03/2025 736/21 Mly 06/2026 ST 1015.00 3 0.00 3045 228.36
130 467399761 RAMA BHATTACHARYA(LA) 28/10/2007 50/26 Qly 07/2026 356.00 1 0.00 356 17.80
134 468308388 NAYAN CHAKRABORTY-LA 28/04/2008 50/19 Qly 07/2026 461.00 1 0.00 461 23.05
206 137675364 ALOKA DEY 28/08/2017 841/16 Yly 08/2026 LP 8852.00 1 0.00 8852 442.60
245 506868156 D G 29/02/2024 873/20 Qly 08/2026 ST 7500.00 1 0.00 7500 262.50
Page Total Premium : 433839 G S T : 0 Estimated Comn : 46501.46
( Page No : 1 | 468 | LIC02579468 | 17862939172240 | Due Month 08/2026)
FY - First year Prem. ST-> 2nd,3rd Year Prem. LP : Last Premium. MT : Matures after this due.`

func recordsByPolicyNo(t *testing.T, text string) map[string]struct {
	name     string
	premium  float64
	mode     string
	term     int
	plan     string
	flag     string
	dueCount int
	nextDue  time.Time
} {
	t.Helper()

	_, records, _ := parseLICDueList(text)
	out := make(map[string]struct {
		name     string
		premium  float64
		mode     string
		term     int
		plan     string
		flag     string
		dueCount int
		nextDue  time.Time
	}, len(records))

	for _, rec := range records {
		out[rec.PolicyNo] = struct {
			name     string
			premium  float64
			mode     string
			term     int
			plan     string
			flag     string
			dueCount int
			nextDue  time.Time
		}{rec.AssuredName, rec.Premium, rec.Mode, rec.Term, rec.PlanCode, rec.Flag, rec.DueCount, rec.CalculatedNextDueDate}
	}
	return out
}

func TestParseLICDueListReadsEveryRow(t *testing.T) {
	_, records, unparsed := parseLICDueList(realDueListText)

	if len(records) != 11 {
		t.Fatalf("expected 11 parsed rows, got %d", len(records))
	}
	if len(unparsed) != 0 {
		t.Fatalf("expected no unreadable policy numbers, got %v", unparsed)
	}
}

func TestParseLICDueListReadsHeader(t *testing.T) {
	header, _, _ := parseLICDueList(realDueListText)

	if header.AgentCode != "LIC02579468" {
		t.Errorf("agent code: got %q, want %q", header.AgentCode, "LIC02579468")
	}
	if header.AgentName != "BIREN CHANDRA DATTA" {
		t.Errorf("agent name: got %q, want %q", header.AgentName, "BIREN CHANDRA DATTA")
	}
	if header.BranchCode != "468" {
		t.Errorf("branch code: got %q, want %q", header.BranchCode, "468")
	}
	if header.DueMonth != "08/2026" {
		t.Errorf("due month: got %q, want %q", header.DueMonth, "08/2026")
	}
}

// The premium is the column this parser exists to get right: a looser pattern reads the year out of
// the D.o.C or the plan number out of "Pln/Tm" and writes that into a client's policy.
func TestParseLICDueListReadsCorrectPremium(t *testing.T) {
	got := recordsByPolicyNo(t, realDueListText)

	for _, tc := range []struct {
		policyNo string
		premium  float64
		mode     string
		term     int
		plan     string
	}{
		{"530766131", 34493.00, "Half-Yearly", 18, "714"},
		{"479512074", 2944.00, "Half-Yearly", 21, "914"},
		{"509608968", 1015.00, "Monthly", 21, "736"},
		{"464804871", 1131.00, "Quarterly", 25, "14"},
		{"137675364", 8852.00, "Yearly", 16, "841"},
		{"467399761", 356.00, "Quarterly", 26, "50"},
	} {
		rec, ok := got[tc.policyNo]
		if !ok {
			t.Errorf("policy %s was not parsed", tc.policyNo)
			continue
		}
		if rec.premium != tc.premium {
			t.Errorf("policy %s premium: got %v, want %v", tc.policyNo, rec.premium, tc.premium)
		}
		if rec.mode != tc.mode {
			t.Errorf("policy %s mode: got %q, want %q", tc.policyNo, rec.mode, tc.mode)
		}
		if rec.term != tc.term {
			t.Errorf("policy %s term: got %d, want %d", tc.policyNo, rec.term, tc.term)
		}
		if rec.plan != tc.plan {
			t.Errorf("policy %s plan code: got %q, want %q", tc.policyNo, rec.plan, tc.plan)
		}
	}
}

// Names in a real due list carry suffixes and punctuation the agent recognises the record by, and
// can be as short as two letters — none of that may be mangled or dropped.
func TestParseLICDueListKeepsNamesIntact(t *testing.T) {
	got := recordsByPolicyNo(t, realDueListText)

	for policyNo, want := range map[string]string{
		"530766131": "ATANU GHOSAL",
		"530779905": "SARFARAJ SEIKH(LA)",
		"448135468": "BIMAN GHOSH SO-SUBHASH GHOSH",
		"468308388": "NAYAN CHAKRABORTY-LA",
		"506868156": "D G",
	} {
		if got[policyNo].name != want {
			t.Errorf("policy %s name: got %q, want %q", policyNo, got[policyNo].name, want)
		}
	}
}

func TestParseLICDueListReadsFlagsAndDueCounts(t *testing.T) {
	got := recordsByPolicyNo(t, realDueListText)

	// A first-year row carries FY and can have several instalments due at once...
	if got["530766131"].flag != "FY" || got["530766131"].dueCount != 2 {
		t.Errorf("policy 530766131: got flag %q / due %d, want FY / 2", got["530766131"].flag, got["530766131"].dueCount)
	}
	// ...a maturing policy carries MT...
	if got["466874490"].flag != "MT" {
		t.Errorf("policy 466874490 flag: got %q, want MT", got["466874490"].flag)
	}
	// ...and a plain renewal row carries no flag at all, which must not shift the columns along.
	if got["479512074"].flag != "" || got["479512074"].dueCount != 2 {
		t.Errorf("policy 479512074: got flag %q / due %d, want empty / 2", got["479512074"].flag, got["479512074"].dueCount)
	}
}

// Next due date = the day from D.o.C on the FUP month/year, clamped to the length of that month.
func TestParseLICDueListCalculatesNextDueDate(t *testing.T) {
	got := recordsByPolicyNo(t, realDueListText)

	// DOC 06/08/2025 + FUP 02/2026 → 6 Feb 2026.
	if want := time.Date(2026, time.February, 6, 0, 0, 0, 0, time.UTC); !got["530766131"].nextDue.Equal(want) {
		t.Errorf("policy 530766131 next due: got %v, want %v", got["530766131"].nextDue, want)
	}
	// DOC 29/02/2024 + FUP 08/2026 → 29 Aug 2026.
	if want := time.Date(2026, time.August, 29, 0, 0, 0, 0, time.UTC); !got["506868156"].nextDue.Equal(want) {
		t.Errorf("policy 506868156 next due: got %v, want %v", got["506868156"].nextDue, want)
	}
}

// A day that does not exist in the FUP month lands on that month's last day rather than rolling
// into the next one — 31 Jan + FUP 02/2027 must be 28 Feb 2027, not 3 March.
func TestCalculateNextDueDateClampsToShortMonths(t *testing.T) {
	if got, want := calculateNextDueDate("31/01/2020", "02/2027"), time.Date(2027, time.February, 28, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// Leap year keeps the 29th.
	if got, want := calculateNextDueDate("31/01/2020", "02/2028"), time.Date(2028, time.February, 29, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// December must not overflow the year when computing the month length.
	if got, want := calculateNextDueDate("31/12/2020", "12/2026"), time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// A row the parser cannot read completely must be reported, never reconstructed from guesses.
func TestParseLICDueListReportsUnreadableRows(t *testing.T) {
	text := realDueListText + "\n999888777 BROKEN ROW 12/2026 nonsense"

	_, records, unparsed := parseLICDueList(text)

	for _, rec := range records {
		if rec.PolicyNo == "999888777" {
			t.Fatalf("a row missing its columns was parsed anyway: %+v", rec)
		}
	}
	found := false
	for _, policyNo := range unparsed {
		if policyNo == "999888777" {
			found = true
		}
	}
	if !found {
		t.Errorf("unreadable policy 999888777 was not reported, got %v", unparsed)
	}
}

// The same policy number appearing twice (pages repeat totals, files get re-read) must collapse to
// exactly one record so a bulk sync never writes the same policy twice in one run.
func TestParseLICDueListDeduplicatesPolicyNumbers(t *testing.T) {
	text := realDueListText + "\n1 530766131 ATANU GHOSAL 06/08/2025 714/18 Hly 02/2026 FY 34493.00 2 0.00 68986 9485.57"

	_, records, _ := parseLICDueList(text)

	seen := 0
	for _, rec := range records {
		if rec.PolicyNo == "530766131" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("policy 530766131 appeared %d times, want exactly 1", seen)
	}
}
