package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// How much of the product catalog one client may see.
//
// The catalog itself is platform-wide — a super admin curates it and every published product is
// offered to everybody by default. This is the per-client override an admin applies on top:
//
//   - ProductAccessAll      — the whole published catalog. The default, and what a client with no
//     record at all gets, so adding this feature never silently empties anyone's Products tab.
//   - ProductAccessSelected — only the products listed on the record. An empty list is legitimate
//     and means the client sees no products; it is not the same as "never configured".
const (
	ProductAccessAll      = "all"
	ProductAccessSelected = "selected"
)

// IsValidProductAccessMode reports whether mode is one of the two access modes.
func IsValidProductAccessMode(mode string) bool {
	return mode == ProductAccessAll || mode == ProductAccessSelected
}

// ClientProductAccess is one client's catalog override.
//
// It deliberately stores no copy of the client's Agency ID: a super admin can move a client to
// another agency, and an authorization check reading a stale snapshot here would keep letting the
// previous admin edit them. Who may change this is always answered from the client's current
// account.
type ClientProductAccess struct {
	ID     bson.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID bson.ObjectID `bson:"user_id" json:"user_id"`
	// Mode is one of the ProductAccess constants.
	Mode string `bson:"mode" json:"mode"`
	// ProductIDs is meaningful only when Mode is ProductAccessSelected.
	ProductIDs []bson.ObjectID `bson:"product_ids" json:"product_ids"`
	// Who last changed it, kept so the admin screen can say "set by X on Y" rather than presenting
	// a restriction nobody will own.
	UpdatedByID   bson.ObjectID `bson:"updated_by_id,omitempty" json:"updated_by_id,omitempty"`
	UpdatedByName string        `bson:"updated_by_name,omitempty" json:"updated_by_name,omitempty"`
	CreatedAt     time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time     `bson:"updated_at" json:"updated_at"`
}

// ClientProductAccessDTO is what the admin screen reads: the current setting, in the ID form the
// app works with.
type ClientProductAccessDTO struct {
	UserID string `json:"user_id"`
	Mode   string `json:"mode"`
	// ProductIDs is empty unless Mode is "selected".
	ProductIDs []string `json:"product_ids"`
	// SelectedCount saves the app counting, and stays correct when a selected product has since
	// been deleted from the catalog (such IDs are dropped before this is returned).
	SelectedCount int `json:"selected_count"`
	// Configured is false when this client has never been restricted — the app shows "whole
	// catalog" without implying somebody chose it.
	Configured    bool       `json:"configured"`
	UpdatedByName string     `json:"updated_by_name,omitempty"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
}

// SetClientProductAccessDTO is the admin's request: which mode, and — for "selected" — exactly
// which products. ProductIDs is ignored for mode "all", so switching back to the whole catalog
// never has to send a list.
type SetClientProductAccessDTO struct {
	Mode       string   `json:"mode" binding:"required,oneof=all selected"`
	ProductIDs []string `json:"product_ids,omitempty"`
}

// ProductVisibility is the resolved answer the catalog asks for on a client's behalf: either "show
// everything" or "show exactly these".
type ProductVisibility struct {
	// Restricted is false when this caller sees the whole published catalog.
	Restricted bool
	// AllowedIDs is meaningful only when Restricted is true, and may be empty (nothing is visible).
	AllowedIDs []bson.ObjectID
}

// Allows reports whether this caller may see the given product.
func (v ProductVisibility) Allows(productID bson.ObjectID) bool {
	if !v.Restricted {
		return true
	}
	for _, allowed := range v.AllowedIDs {
		if allowed == productID {
			return true
		}
	}
	return false
}

// ClientProductAccessRepository stores the per-client catalog overrides.
type ClientProductAccessRepository interface {
	// FindByUserID returns the client's override, or nil when they have none (meaning unrestricted).
	FindByUserID(ctx context.Context, userID bson.ObjectID) (*ClientProductAccess, error)
	// Upsert writes the client's override, creating it the first time.
	Upsert(ctx context.Context, access *ClientProductAccess) (*ClientProductAccess, error)
	// DeleteByUserID removes a client's override — used when the account itself goes away.
	DeleteByUserID(ctx context.Context, userID bson.ObjectID) error
	// RemoveProductFromAll drops a deleted product from every client's list, so no override keeps
	// pointing at a product that no longer exists.
	RemoveProductFromAll(ctx context.Context, productID bson.ObjectID) error
}

// ClientProductAccessService is the admin-facing control over what one client sees in the catalog.
type ClientProductAccessService interface {
	// GetForClient returns the client's current setting. Agency staff only, and a plain admin only
	// for a client of their own agency.
	GetForClient(ctx context.Context, requesterRole, requesterID, targetUserID string) (*ClientProductAccessDTO, error)
	// SetForClient replaces the client's setting. Every product ID is verified to exist, so a typo
	// can't quietly hide a product the admin believed they had granted.
	SetForClient(ctx context.Context, requesterRole, requesterID, targetUserID string, dto *SetClientProductAccessDTO) (*ClientProductAccessDTO, error)
	// ResolveVisibility answers what this account may see in the catalog. Staff are never
	// restricted; a client with no override sees everything.
	ResolveVisibility(ctx context.Context, requesterRole, requesterID string) (ProductVisibility, error)
}
