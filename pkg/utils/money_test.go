package utils

import (
	"testing"
)

func TestRoundCurrency(t *testing.T) {
	// Standard floating-point drift accumulation
	var sum float64
	for i := 0; i < 10; i++ {
		sum += 0.1
	}
	// In IEEE 754 float64, sum of ten 0.1s is 0.9999999999999999
	rounded := RoundCurrency(sum)
	if rounded != 1.0 {
		t.Errorf("expected RoundCurrency to round accumulated 0.1s to 1.0, got %v", rounded)
	}

	tests := []struct {
		input float64
		want  float64
	}{
		{123.456, 123.46},
		{123.454, 123.45},
		{123.455, 123.46},
		{0.0, 0.0},
		{-50.555, -50.56},
		{999999.999, 1000000.0},
	}

	for _, tc := range tests {
		got := RoundCurrency(tc.input)
		if got != tc.want {
			t.Errorf("RoundCurrency(%v) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestFormatRupees(t *testing.T) {
	tests := []struct {
		amount          float64
		includeDecimals bool
		want            string
	}{
		{0, false, "Rs. 0"},
		{500, false, "Rs. 500"},
		{1200, false, "Rs. 1,200"},
		{1234567, false, "Rs. 12,34,567"},
		{10000000, false, "Rs. 1,00,00,000"},
		{-1234567, false, "Rs. -12,34,567"},
		{1234.50, true, "Rs. 1,234.50"},
		{1234.00, true, "Rs. 1,234"},
	}

	for _, tc := range tests {
		got := FormatRupees(tc.amount, tc.includeDecimals)
		if got != tc.want {
			t.Errorf("FormatRupees(%v, %v) = %q, want %q", tc.amount, tc.includeDecimals, got, tc.want)
		}
	}
}
