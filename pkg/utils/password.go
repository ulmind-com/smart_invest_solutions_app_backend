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
