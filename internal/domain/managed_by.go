package domain

// Who keeps a client's record up to date. It is stamped when the record is created — from the role
// of whoever filed it — and answers a question the client actually asks: "do I have to maintain
// this, or does my advisor?"
//
// This is deliberately separate from IsMapped. IsMapped is the agency's own book-keeping flag
// ("counted in the agency portfolio"), which an admin can set or clear for a record either side
// maintains; ManagedBy is provenance, and is what the client app shows.
const (
	// ManagedByClient — the client added this record themselves and maintains it.
	ManagedByClient = "client"
	// ManagedByAgency — agency staff filed it (directly or from an LIC due list) and maintain it.
	ManagedByAgency = "agency"
)

// ManagedByForRole returns the provenance to stamp on a record created by this role.
func ManagedByForRole(role string) string {
	if role == RoleAdmin || role == RoleSuperAdmin {
		return ManagedByAgency
	}
	return ManagedByClient
}

// IsValidManagedBy reports whether value is one of the two provenance values.
func IsValidManagedBy(value string) bool {
	return value == ManagedByClient || value == ManagedByAgency
}

// NormalizeManagedBy defaults a missing value (records written before this field existed, read
// straight from the database) to client-managed.
func NormalizeManagedBy(value string) string {
	if IsValidManagedBy(value) {
		return value
	}
	return ManagedByClient
}
