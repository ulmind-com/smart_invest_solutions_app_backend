package utils

import (
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// NormalizeEmail trims surrounding whitespace and lower-cases an address so a
// value written by one code path is found by another. Addresses are stored
// normalized; the filter below is what reaches rows written before that was true.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// EmailFilter builds a case-insensitive exact-match filter for an email field.
//
// Mongo compares strings byte for byte, so a row stored as "Rahul@Gmail.com"
// is invisible to a query for "rahul@gmail.com". Every lookup that used the
// address as typed therefore behaved as though the account did not exist —
// which, on the password reset path, is indistinguishable from success.
func EmailFilter(email string) bson.M {
	return bson.M{"email": bson.M{
		"$regex":   "^" + regexp.QuoteMeta(NormalizeEmail(email)) + "$",
		"$options": "i",
	}}
}
