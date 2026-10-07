package domain

import (
	"context"
	"io"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Who an announcement is shown to.
//
// "Everyone" is the common case — a seasonal offer belongs on every screen. The other two exist
// because the alternative is worse than it looks: an offer written for clients ("renew your policy
// and get…") shown on an admin's dashboard is clutter they can't act on, and an internal notice to
// staff shown to clients is a leak.
const (
	AnnouncementAudienceAll     = "all"
	AnnouncementAudienceClients = "clients"
	AnnouncementAudienceAdmins  = "admins"
)

// IsValidAnnouncementAudience reports whether value is one of the three audiences.
func IsValidAnnouncementAudience(value string) bool {
	switch value {
	case AnnouncementAudienceAll, AnnouncementAudienceClients, AnnouncementAudienceAdmins:
		return true
	}
	return false
}

// What kind of media an announcement carries.
const (
	AnnouncementMediaNone  = ""
	AnnouncementMediaImage = "image"
	AnnouncementMediaVideo = "video"
)

// Announcement is a banner a Super Admin puts on everyone's screen — a seasonal offer, a new product,
// a notice. Curated centrally and platform-wide: a single agency does not get to broadcast.
type Announcement struct {
	ID          bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Title       string        `bson:"title" json:"title"`
	Description string        `bson:"description,omitempty" json:"description,omitempty"`

	// MediaURL is the image or video to show. MediaType says which, and is also the storage resource
	// type the asset must be deleted with — a video removed as an "image" is silently kept forever.
	MediaURL      string `bson:"media_url,omitempty" json:"media_url,omitempty"`
	MediaPublicID string `bson:"media_public_id,omitempty" json:"media_public_id,omitempty"`
	MediaType     string `bson:"media_type,omitempty" json:"media_type,omitempty"`
	// ThumbnailURL is a still frame for a video, so the banner shows something before anyone presses
	// play. Empty for an image, which is its own thumbnail.
	ThumbnailURL string `bson:"thumbnail_url,omitempty" json:"thumbnail_url,omitempty"`

	// LinkURL is an optional tap-through, and CTALabel the words on the button.
	LinkURL  string `bson:"link_url,omitempty" json:"link_url,omitempty"`
	CTALabel string `bson:"cta_label,omitempty" json:"cta_label,omitempty"`

	Audience string `bson:"audience" json:"audience"`
	IsActive bool   `bson:"is_active" json:"is_active"`

	// StartsAt and EndsAt bound when this is live. Both optional: nil start means "from now", nil end
	// means "until switched off". A dated offer that can expire on its own is the point — otherwise
	// somebody has to remember to take Diwali down in November.
	StartsAt *time.Time `bson:"starts_at,omitempty" json:"starts_at,omitempty"`
	EndsAt   *time.Time `bson:"ends_at,omitempty" json:"ends_at,omitempty"`

	// Priority orders the banners when more than one is live, highest first.
	Priority int `bson:"priority" json:"priority"`

	CreatedByID   bson.ObjectID `bson:"created_by_id,omitempty" json:"created_by_id,omitempty"`
	CreatedByName string        `bson:"created_by_name,omitempty" json:"created_by_name,omitempty"`
	CreatedAt     time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time     `bson:"updated_at" json:"updated_at"`
}

// AnnouncementStatus is the live state of an announcement, worked out from its flag and its window
// rather than stored — so it can never disagree with the dates beside it on screen.
const (
	AnnouncementStatusLive      = "live"
	AnnouncementStatusScheduled = "scheduled"
	AnnouncementStatusExpired   = "expired"
	AnnouncementStatusPaused    = "paused"
)

// AnnouncementAdminView is one row of the Super Admin's management list: the record plus the state
// the viewer would otherwise have to derive from three fields.
type AnnouncementAdminView struct {
	*Announcement
	// Status is one of the AnnouncementStatus constants.
	Status string `json:"status"`
}

// CreateAnnouncementDTO is the payload for a new announcement. Populated from multipart form fields,
// with the media file handled separately as a reader — the same shape as a product's brochure.
type CreateAnnouncementDTO struct {
	Title       string `form:"title" json:"title" binding:"required"`
	Description string `form:"description" json:"description,omitempty"`
	LinkURL     string `form:"link_url" json:"link_url,omitempty"`
	CTALabel    string `form:"cta_label" json:"cta_label,omitempty"`
	Audience    string `form:"audience" json:"audience,omitempty"`
	// IsActive nil defaults to true: somebody filling in this form means to publish.
	IsActive *bool      `form:"is_active" json:"is_active,omitempty"`
	StartsAt *time.Time `form:"starts_at" json:"starts_at,omitempty"`
	EndsAt   *time.Time `form:"ends_at" json:"ends_at,omitempty"`
	Priority *int       `form:"priority" json:"priority,omitempty"`
}

// UpdateAnnouncementDTO patches an existing announcement — only non-nil fields change.
//
// ClearMedia and ClearSchedule exist because a nil pointer already means "leave alone": without them
// there would be no way to say "remove the picture" or "make this run indefinitely".
type UpdateAnnouncementDTO struct {
	Title       *string `form:"title" json:"title,omitempty"`
	Description *string `form:"description" json:"description,omitempty"`
	LinkURL     *string `form:"link_url" json:"link_url,omitempty"`
	CTALabel    *string `form:"cta_label" json:"cta_label,omitempty"`
	Audience    *string `form:"audience" json:"audience,omitempty"`
	IsActive    *bool   `form:"is_active" json:"is_active,omitempty"`
	StartsAt    *time.Time
	EndsAt      *time.Time
	Priority    *int `form:"priority" json:"priority,omitempty"`
	// ClearMedia removes the image or video, leaving a text-only banner.
	ClearMedia bool `json:"-"`
	// ClearStart / ClearEnd drop a bound, so the banner runs from now / until switched off.
	ClearStart bool `json:"-"`
	ClearEnd   bool `json:"-"`

	// Set by the service after a successful upload — never read from the request.
	MediaURL      *string `json:"-"`
	MediaPublicID *string `json:"-"`
	MediaType     *string `json:"-"`
	ThumbnailURL  *string `json:"-"`
}

// MediaUpload is an optional file arriving with an announcement.
//
// A struct rather than a loose (io.Reader, string) pair because "no file" is the normal case on an
// update — a nil Reader inside a value is harder to misread than two parameters that must be nil or
// non-nil together.
type MediaUpload struct {
	File     io.Reader
	Filename string
}

// Present reports whether a file was actually supplied.
func (m MediaUpload) Present() bool { return m.File != nil }

// AnnouncementRepository defines database operations for announcements.
type AnnouncementRepository interface {
	Create(ctx context.Context, announcement *Announcement) (*Announcement, error)
	FindByID(ctx context.Context, id bson.ObjectID) (*Announcement, error)
	// FindLive returns the announcements that should be on screen right now for this audience:
	// active, inside their window at `now`, ordered highest priority then newest. The filtering is
	// done in the query so a draft or an expired banner never reaches a device at all.
	FindLive(ctx context.Context, audiences []string, now time.Time) ([]*Announcement, error)
	// FindAll returns every announcement, newest first — the Super Admin's management list.
	FindAll(ctx context.Context, page, limit int64) ([]*Announcement, int64, error)
	Update(ctx context.Context, id bson.ObjectID, dto *UpdateAnnouncementDTO) (*Announcement, error)
	Delete(ctx context.Context, id bson.ObjectID) error
}

// AnnouncementService defines business logic for announcements.
type AnnouncementService interface {
	// GetForMe returns the banners this caller should see, resolved from their role.
	GetForMe(ctx context.Context, requesterRole string) ([]*Announcement, error)
	// GetAll returns every announcement with its derived status. Super Admin only.
	GetAll(ctx context.Context, requesterRole string, page, limit int64) ([]*AnnouncementAdminView, int64, error)
	// Create adds an announcement, uploading the optional media file. Super Admin only.
	Create(ctx context.Context, requesterRole, requesterID string, dto *CreateAnnouncementDTO, media MediaUpload) (*Announcement, error)
	// Update patches an announcement, replacing the media when a new file is supplied. Super Admin only.
	Update(ctx context.Context, requesterRole, idStr string, dto *UpdateAnnouncementDTO, media MediaUpload) (*Announcement, error)
	// Delete removes an announcement and purges its media. Super Admin only.
	Delete(ctx context.Context, requesterRole, idStr string) error
}
