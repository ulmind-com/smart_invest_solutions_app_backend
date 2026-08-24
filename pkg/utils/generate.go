package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// adminIDCharset excludes visually ambiguous characters (0/O, 1/I/L) to reduce transcription errors
// when a Super Admin reads out or types an Admin ID.
const adminIDCharset = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// GenerateAdminID creates a random, human-friendly Admin ID in the form "ADM-XXXXXX".
func GenerateAdminID() (string, error) {
	charsetLength := big.NewInt(int64(len(adminIDCharset)))

	b := make([]byte, 6)
	for i := range b {
		num, err := rand.Int(rand.Reader, charsetLength)
		if err != nil {
			return "", err
		}
		b[i] = adminIDCharset[num.Int64()]
	}

	return fmt.Sprintf("ADM-%s", string(b)), nil
}

// GenerateTicketNumber creates a random 5-digit support ticket number in the form "TKT-XXXXX".
// Callers are expected to retry with a fresh candidate on a duplicate-key error, since uniqueness
// is enforced by the collection's unique index rather than by this generator.
func GenerateTicketNumber() (string, error) {
	code, err := GenerateNumericCode(5)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("TKT-%s", code), nil
}

// GenerateNumericCode creates a cryptographically secure numeric code (e.g. PIN/OTP) with the given
// number of digits, zero-padded on the left.
func GenerateNumericCode(digits int) (string, error) {
	if digits <= 0 {
		digits = 4
	}

	max := big.NewInt(1)
	ten := big.NewInt(10)
	for i := 0; i < digits; i++ {
		max.Mul(max, ten)
	}

	num, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%0*d", digits, num.Int64()), nil
}
