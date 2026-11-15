package utils

import (
	"fmt"
	"math"
	"strings"
)

// RoundCurrency rounds a financial amount to two decimal places (exact paisa).
// It prevents floating-point drift (e.g. 0.1 + 0.2 = 0.30000000000000004).
func RoundCurrency(val float64) float64 {
	return math.Round(val*100) / 100
}

// FormatRupees formats a monetary amount using Indian digit grouping (e.g. "Rs. 12,34,567").
// If includeDecimals is true, paisa are appended when non-zero (e.g. "Rs. 12,34,567.50").
func FormatRupees(amount float64, includeDecimals bool) string {
	rounded := RoundCurrency(amount)
	sign := ""
	if rounded < 0 {
		sign = "-"
		rounded = -rounded
	}

	intPart := int64(math.Floor(rounded))
	paisa := int64(math.Round((rounded - float64(intPart)) * 100))
	if paisa >= 100 {
		intPart++
		paisa = 0
	}

	digits := fmt.Sprintf("%d", intPart)
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

	if includeDecimals && paisa > 0 {
		return fmt.Sprintf("Rs. %s%s.%02d", sign, digits, paisa)
	}
	return "Rs. " + sign + digits
}
