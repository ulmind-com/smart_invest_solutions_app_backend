package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Paging bounds for the renewal list. The page is large because this screen is worked through, not
// browsed — an admin calling clients wants the whole week in front of them.
const (
	renewalDefaultLimit = 50
	renewalMaxLimit     = 200
)

type renewalService struct {
	renewalRepo domain.RenewalRepository
	userRepo    domain.UserRepository
}

// NewRenewalService creates the renewal service.
func NewRenewalService(renewalRepo domain.RenewalRepository, userRepo domain.UserRepository) domain.RenewalService {
	return &renewalService{renewalRepo: renewalRepo, userRepo: userRepo}
}

// GetRenewals answers "what needs chasing, for whom, and under which admin".
//
// The four instruments are read separately — each stores its date differently — then merged into one
// list ordered by urgency, which is the order the work actually gets done in. The window and horizon
// bound the fetch in the database, so the merge happens over the rows that could appear on screen
// rather than over the whole book.
func (s *renewalService) GetRenewals(ctx context.Context, requesterRole, requesterID string, query domain.RenewalQuery) (*domain.RenewalPage, error) {
	query = normalizeRenewalQuery(query)

	agencyFilter, allowed := resolveListingAgencyID(ctx, s.userRepo, requesterRole, requesterID, query.AgencyID)
	if !allowed {
		return emptyRenewalPage(query), nil
	}

	now := time.Now()
	today := dayNumber(now)

	// The fetch always covers the full horizon, whatever window is on screen: the summary counts
	// every bucket, so narrowing the query to the selected window would make the other counts lie.
	from := dayStart(now.AddDate(0, 0, -premiumOverdueLookback))
	to := dayEnd(now.AddDate(0, 0, domain.RenewalHorizonDays))

	rows, err := s.collectRows(ctx, from, to, agencyFilter, today)
	if err != nil {
		return nil, err
	}

	// Summary before the window and kind filters, so the tallies describe the whole scope and hold
	// still as the reader switches between them.
	summary := summarizeRenewals(rows)

	rows = filterRenewals(rows, query)
	sortRenewals(rows)

	total := int64(len(rows))
	page := paginateRenewals(rows, query.Page, query.Limit)

	return &domain.RenewalPage{
		Items:      page,
		Summary:    summary,
		Total:      total,
		Page:       query.Page,
		Limit:      query.Limit,
		TotalPages: renewalTotalPages(total, query.Limit),
	}, nil
}

// collectRows reads all four instruments and flattens them into one shape.
func (s *renewalService) collectRows(ctx context.Context, from, to time.Time, agencyFilter string, today int) ([]*domain.RenewalRow, error) {
	rows := make([]*domain.RenewalRow, 0, 64)

	life, err := s.renewalRepo.LifeDue(ctx, from, to, agencyFilter)
	if err != nil {
		return nil, err
	}
	for _, p := range life {
		// Premiums stop at the end of the premium paying term, or at maturity, whichever comes first.
		// A due date past that is a stale schedule rather than a real demand — the same rule the
		// client's own dashboard applies, so the two never disagree about what is owed.
		coverEnds := p.PolicyDetails.MaturityDate
		if p.PolicyDetails.PPT > 0 && !p.PolicyDetails.DOC.IsZero() {
			if pptEnd := p.PolicyDetails.DOC.AddDate(p.PolicyDetails.PPT, 0, 0); coverEnds.IsZero() || pptEnd.Before(coverEnds) {
				coverEnds = pptEnd
			}
		}
		due := p.PremiumDetails.NextDueDate
		if due.IsZero() || (!coverEnds.IsZero() && dayNumber(due) > dayNumber(coverEnds)) {
			continue
		}

		rows = append(rows, newRenewalRow(renewalRowInput{
			kind:        domain.RenewalKindLife,
			recordID:    p.ID,
			title:       fallback(p.PolicyDetails.PlanName, "Life policy"),
			reference:   p.PolicyDetails.PolicyNo,
			company:     p.CompanyName,
			insured:     p.PolicyDetails.LifeInsuredName,
			amount:      p.PremiumDetails.InstallmentPremium,
			label:       "Premium due",
			due:         due,
			today:       today,
			userID:      p.UserID,
			clientName:  p.CustomerName,
			clientPhone: p.ContactNo,
			agencyID:    p.AgencyID,
			managedBy:   p.ManagedBy,
		}))
	}

	health, err := s.renewalRepo.HealthDue(ctx, from, to, agencyFilter)
	if err != nil {
		return nil, err
	}
	for _, p := range health {
		// A health policy can need acting on for two reasons. Whichever comes first is the one worth
		// showing: chasing a premium that is due next week is pointless if the cover lapses tomorrow.
		due, label, amount := p.PremiumDetails.NextDueDate, "Premium due", p.PremiumDetails.InstallmentPremium
		expiry := p.PolicyDetails.ExpiryDate
		premiumUsable := !due.IsZero() && (expiry.IsZero() || dayNumber(due) <= dayNumber(expiry))
		if !premiumUsable || (!expiry.IsZero() && dayNumber(expiry) < dayNumber(due)) {
			due, label, amount = expiry, "Policy expires", 0
		}
		if due.IsZero() || dayNumber(due) < dayNumber(from) || dayNumber(due) > dayNumber(to) {
			continue
		}

		rows = append(rows, newRenewalRow(renewalRowInput{
			kind:        domain.RenewalKindHealth,
			recordID:    p.ID,
			title:       fallback(p.PolicyDetails.PlanName, "Health policy"),
			reference:   p.PolicyDetails.PolicyNo,
			company:     p.CompanyName,
			insured:     p.PolicyDetails.PrimaryInsuredName,
			amount:      amount,
			label:       label,
			due:         due,
			today:       today,
			userID:      p.UserID,
			clientName:  p.CustomerName,
			clientPhone: p.ContactNo,
			agencyID:    p.AgencyID,
			managedBy:   p.ManagedBy,
		}))
	}

	motor, err := s.renewalRepo.MotorExpiring(ctx, isoDate(from), isoDate(to), agencyFilter)
	if err != nil {
		return nil, err
	}
	for _, p := range motor {
		due, parseErr := time.ParseInLocation("2006-01-02", p.DateOfExpiry, indiaTZ)
		if parseErr != nil {
			continue
		}
		rows = append(rows, newRenewalRow(renewalRowInput{
			kind:        domain.RenewalKindMotor,
			recordID:    p.ID,
			title:       p.VehicleNo,
			reference:   p.PolicyNo,
			company:     p.CompanyName,
			label:       "Policy expires",
			due:         due,
			today:       today,
			userID:      p.UserID,
			clientName:  p.CustomerName,
			clientPhone: p.ContactNo,
			agencyID:    p.AgencyID,
			managedBy:   p.ManagedBy,
		}))
	}

	deposits, err := s.renewalRepo.DepositsMaturing(ctx, from, to, agencyFilter)
	if err != nil {
		return nil, err
	}
	for _, d := range deposits {
		if d.MaturityDate.IsZero() {
			continue
		}
		rows = append(rows, newRenewalRow(renewalRowInput{
			kind:        domain.RenewalKindDeposit,
			recordID:    d.ID,
			title:       fallback(d.FDName, "Deposit"),
			reference:   d.FDNumber,
			company:     d.CompanyName,
			amount:      d.MaturityAmount,
			label:       "Matures",
			due:         d.MaturityDate,
			today:       today,
			userID:      d.UserID,
			clientName:  d.CustomerName,
			clientPhone: d.ContactNo,
			agencyID:    d.AgencyID,
			managedBy:   d.ManagedBy,
		}))
	}

	return s.nameAgencies(ctx, rows), nil
}

// renewalRowInput is the per-instrument data one row is built from.
type renewalRowInput struct {
	kind        string
	recordID    bson.ObjectID
	title       string
	reference   string
	company     string
	insured     string
	amount      float64
	label       string
	due         time.Time
	today       int
	userID      bson.ObjectID
	clientName  string
	clientPhone string
	agencyID    string
	managedBy   string
}

func newRenewalRow(in renewalRowInput) *domain.RenewalRow {
	daysLeft := dayNumber(in.due) - in.today
	return &domain.RenewalRow{
		Kind:        in.kind,
		RecordID:    in.recordID.Hex(),
		Title:       in.title,
		Reference:   in.reference,
		Company:     in.company,
		InsuredName: in.insured,
		Amount:      in.amount,
		Label:       in.label,
		DueDate:     in.due,
		DaysLeft:    daysLeft,
		IsOverdue:   daysLeft < 0,
		ClientID:    in.userID.Hex(),
		ClientName:  fallback(in.clientName, "Client"),
		ClientPhone: in.clientPhone,
		AgencyID:    in.agencyID,
		ManagedBy:   domain.NormalizeManagedBy(in.managedBy),
	}
}

// nameAgencies fills in the admin behind each row's Agency ID.
//
// Looked up once per distinct agency rather than once per row: a book of renewals belongs to a
// handful of admins, and a lookup per row would be the same query hundreds of times.
func (s *renewalService) nameAgencies(ctx context.Context, rows []*domain.RenewalRow) []*domain.RenewalRow {
	names := map[string]string{}
	for _, row := range rows {
		if row.AgencyID == "" {
			continue
		}
		name, seen := names[row.AgencyID]
		if !seen {
			if owner, err := s.userRepo.FindByAdminID(ctx, row.AgencyID); err == nil && owner != nil {
				name = owner.Name
			}
			names[row.AgencyID] = name
		}
		row.AgencyName = name
	}
	return rows
}

// summarizeRenewals counts the whole scope: how much is overdue, what is coming, and in what.
func summarizeRenewals(rows []*domain.RenewalRow) *domain.RenewalSummary {
	summary := &domain.RenewalSummary{}
	for _, row := range rows {
		switch {
		case row.IsOverdue:
			summary.Overdue++
			summary.AmountOverdue += row.Amount
		default:
			// Cumulative buckets: a row due in 3 days counts in 7, 30 and 90.
			if row.DaysLeft <= 7 {
				summary.DueIn7++
			}
			if row.DaysLeft <= 30 {
				summary.DueIn30++
				summary.AmountDueIn30 += row.Amount
			}
			if row.DaysLeft <= domain.RenewalHorizonDays {
				summary.DueIn90++
			}
		}

		switch row.Kind {
		case domain.RenewalKindLife:
			summary.Life++
		case domain.RenewalKindHealth:
			summary.Health++
		case domain.RenewalKindMotor:
			summary.Motor++
		case domain.RenewalKindDeposit:
			summary.Deposits++
		}

		if row.AgencyID == "" {
			summary.Unassigned++
		}
	}
	return summary
}

// filterRenewals applies the window, instrument and search narrowing that the summary ignores.
func filterRenewals(rows []*domain.RenewalRow, query domain.RenewalQuery) []*domain.RenewalRow {
	needle := strings.ToLower(query.Search)
	out := make([]*domain.RenewalRow, 0, len(rows))

	for _, row := range rows {
		if query.Kind != "" && row.Kind != query.Kind {
			continue
		}
		if !renewalInWindow(row, query.Window) {
			continue
		}
		if needle != "" && !renewalMatches(row, needle) {
			continue
		}
		out = append(out, row)
	}
	return out
}

// renewalInWindow reports whether a row belongs in the selected window. The day windows include
// everything overdue as well: an admin working "the next 7 days" has to see what they already missed,
// or the list quietly hides the most urgent work.
func renewalInWindow(row *domain.RenewalRow, window string) bool {
	switch window {
	case domain.RenewalWindowOverdue:
		return row.IsOverdue
	case domain.RenewalWindow7:
		return row.DaysLeft <= 7
	case domain.RenewalWindow30:
		return row.DaysLeft <= 30
	case domain.RenewalWindow90:
		return row.DaysLeft <= domain.RenewalHorizonDays
	default:
		return true
	}
}

// renewalMatches searches the fields somebody would actually type: who it is, what it is, and the
// number on the paperwork.
func renewalMatches(row *domain.RenewalRow, needle string) bool {
	for _, field := range []string{
		row.ClientName, row.ClientPhone, row.Title, row.Reference,
		row.InsuredName, row.Company, row.AgencyName, row.AgencyID,
	} {
		if field != "" && strings.Contains(strings.ToLower(field), needle) {
			return true
		}
	}
	return false
}

// sortRenewals puts the work in the order it gets done: most overdue first, then soonest due. Ties
// break on the client's name so the order is stable rather than arbitrary.
func sortRenewals(rows []*domain.RenewalRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].DaysLeft != rows[j].DaysLeft {
			return rows[i].DaysLeft < rows[j].DaysLeft
		}
		if rows[i].ClientName != rows[j].ClientName {
			return rows[i].ClientName < rows[j].ClientName
		}
		return rows[i].RecordID < rows[j].RecordID
	})
}

// paginateRenewals cuts the requested page out of the sorted list, tolerating a page past the end.
func paginateRenewals(rows []*domain.RenewalRow, page, limit int64) []*domain.RenewalRow {
	start := (page - 1) * limit
	if start >= int64(len(rows)) {
		return []*domain.RenewalRow{}
	}
	end := start + limit
	if end > int64(len(rows)) {
		end = int64(len(rows))
	}
	return rows[start:end]
}

func renewalTotalPages(total, limit int64) int64 {
	if limit < 1 {
		return 1
	}
	pages := total / limit
	if total%limit != 0 {
		pages++
	}
	if pages < 1 {
		return 1
	}
	return pages
}

// normalizeRenewalQuery clamps paging and drops values the service wouldn't recognise, so an unknown
// filter reads as "no filter" instead of silently matching nothing.
func normalizeRenewalQuery(query domain.RenewalQuery) domain.RenewalQuery {
	query.AgencyID = strings.TrimSpace(query.AgencyID)
	query.Search = strings.TrimSpace(query.Search)

	kind := strings.ToLower(strings.TrimSpace(query.Kind))
	if !domain.IsValidRenewalKind(kind) {
		kind = ""
	}
	query.Kind = kind

	switch strings.ToLower(strings.TrimSpace(query.Window)) {
	case domain.RenewalWindowOverdue:
		query.Window = domain.RenewalWindowOverdue
	case domain.RenewalWindow7:
		query.Window = domain.RenewalWindow7
	case domain.RenewalWindow30:
		query.Window = domain.RenewalWindow30
	case domain.RenewalWindow90:
		query.Window = domain.RenewalWindow90
	default:
		query.Window = domain.RenewalWindowAll
	}

	if query.Page < 1 {
		query.Page = 1
	}
	if query.Limit < 1 || query.Limit > renewalMaxLimit {
		query.Limit = renewalDefaultLimit
	}

	return query
}

// emptyRenewalPage is the "nothing to show" answer — never nil slices, so the app renders an empty
// list rather than failing to read a null.
func emptyRenewalPage(query domain.RenewalQuery) *domain.RenewalPage {
	return &domain.RenewalPage{
		Items:      []*domain.RenewalRow{},
		Summary:    &domain.RenewalSummary{},
		Page:       query.Page,
		Limit:      query.Limit,
		TotalPages: 1,
	}
}

// dayStart and dayEnd widen a window to whole Indian calendar days, so a record due later today is
// never excluded by the time of day the request happens to be made.
func dayStart(t time.Time) time.Time {
	y, m, d := t.In(indiaTZ).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, indiaTZ)
}

func dayEnd(t time.Time) time.Time {
	y, m, d := t.In(indiaTZ).Date()
	return time.Date(y, m, d, 23, 59, 59, int(time.Second-time.Nanosecond), indiaTZ)
}

// isoDate renders a window bound the way motor expiry dates are stored.
func isoDate(t time.Time) string {
	return t.In(indiaTZ).Format("2006-01-02")
}

func fallback(value, alternative string) string {
	if strings.TrimSpace(value) == "" {
		return alternative
	}
	return value
}
