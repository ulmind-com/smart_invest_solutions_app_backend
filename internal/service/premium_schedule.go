package service

import (
	"fmt"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
)

// monthsPerInstallment is how far one premium payment moves the schedule for each payment mode.
func monthsPerInstallment(mode string) (int, error) {
	switch mode {
	case domain.PaymentModeYearly:
		return 12, nil
	case domain.PaymentModeHalfYearly:
		return 6, nil
	case domain.PaymentModeQuarterly:
		return 3, nil
	case domain.PaymentModeMonthly:
		return 1, nil
	default:
		return 0, fmt.Errorf("unknown payment mode %q — edit the policy and pick a payment mode first", mode)
	}
}

// addMonthsClamped adds months to t, pinning the result to anchorDay (the policy's own due day)
// clamped to the target month's length. time.AddDate would instead roll 31 Jan + 1 month over to
// 3 Mar, and a schedule that once slid to the 28th would stay there forever.
func addMonthsClamped(t time.Time, months, anchorDay int) time.Time {
	y, m, _ := t.Date()
	firstOfTarget := time.Date(y, m+time.Month(months), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	lastDay := firstOfTarget.AddDate(0, 1, -1).Day()
	day := anchorDay
	if day < 1 {
		day = t.Day()
	}
	if day > lastDay {
		day = lastDay
	}
	return firstOfTarget.AddDate(0, 0, day-1)
}

// nextPremiumDueDate is the due date that follows paying the installment due on current. anchor is
// the date whose day-of-month the schedule follows (the date of commencement when known).
func nextPremiumDueDate(current time.Time, mode string, anchor time.Time) (time.Time, error) {
	if current.IsZero() {
		return time.Time{}, fmt.Errorf("this policy has no next due date to advance — edit the policy and set one")
	}
	months, err := monthsPerInstallment(mode)
	if err != nil {
		return time.Time{}, err
	}
	anchorDay := current.Day()
	if !anchor.IsZero() {
		anchorDay = anchor.Day()
	}
	return addMonthsClamped(current, months, anchorDay), nil
}
