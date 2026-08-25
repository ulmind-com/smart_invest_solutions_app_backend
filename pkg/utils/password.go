package utils

import (
	"crypto/rand"
	"math/big"
)

const (
	charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
)

// GenerateRandomPassword creates a cryptographically secure random password of the given length.
func GenerateRandomPassword(length int) (string, error) {
	if length <= 0 {
		length = 10
	}

	b := make([]byte, length)
	charsetLength := big.NewInt(int64(len(charset)))

	for i := range b {
		num, err := rand.Int(rand.Reader, charsetLength)
		if err != nil {
			return "", err
		}
		b[i] = charset[num.Int64()]
	}

	return string(b), nil
}

const alphaNumericCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GenerateReferralCode creates a cryptographically secure uppercase alphanumeric referral code.
func GenerateReferralCode(length int) (string, error) {
	if length <= 0 {
		length = 6
	}

	b := make([]byte, length)
	charsetLength := big.NewInt(int64(len(alphaNumericCharset)))

	for i := range b {
		num, err := rand.Int(rand.Reader, charsetLength)
		if err != nil {
			return "", err
		}
		b[i] = alphaNumericCharset[num.Int64()]
	}

	return string(b), nil
}
