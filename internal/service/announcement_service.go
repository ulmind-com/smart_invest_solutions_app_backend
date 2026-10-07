package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// announcementMediaFolder is where banner images and videos are stored.
const announcementMediaFolder = "smart_invest_announcements"

// Field limits. A banner is read at a glance on a phone, so these are the lengths that still render
// as a banner rather than as a wall of text — and they stop a crafted request storing an essay.
const (
	maxAnnouncementTitleLength       = 80
	maxAnnouncementDescriptionLength = 280
	maxAnnouncementCTALength         = 24
)

// Paging bounds for the Super Admin's management list.
const (
	announcementDefaultLimit = 20
	announcementMaxLimit     = 100
)

type announcementService struct {
	repo       domain.AnnouncementRepository
	userRepo   domain.UserRepository
	storageSvc StorageService
}

// NewAnnouncementService creates the announcement service.
func NewAnnouncementService(
	repo domain.AnnouncementRepository,
	userRepo domain.UserRepository,
	storageSvc StorageService,
) domain.AnnouncementService {
	return &announcementService{repo: repo, userRepo: userRepo, storageSvc: storageSvc}
}

// GetForMe returns the banners this caller should see.
//
// The audiences are resolved from the caller's own role, never from the request: a client asking for
// the admins' notices must get the clients' ones.
func (s *announcementService) GetForMe(ctx context.Context, requesterRole string) ([]*domain.Announcement, error) {
	audiences := []string{domain.AnnouncementAudienceAll}
	if isAgencyStaff(requesterRole) {
		audiences = append(audiences, domain.AnnouncementAudienceAdmins)
	} else {
		audiences = append(audiences, domain.AnnouncementAudienceClients)
	}

	return s.repo.FindLive(ctx, audiences, time.Now().UTC())
}

// GetAll returns every announcement with its live status. Super Admin only.
func (s *announcementService) GetAll(ctx context.Context, requesterRole string, page, limit int64) ([]*domain.AnnouncementAdminView, int64, error) {
	if !isSuperAdmin(requesterRole) {
		return nil, 0, fmt.Errorf("access denied: only a super admin manages announcements")
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > announcementMaxLimit {
		limit = announcementDefaultLimit
	}

	rows, total, err := s.repo.FindAll(ctx, page, limit)
	if err != nil {
		return nil, 0, err
	}

	now := time.Now().UTC()
	views := make([]*domain.AnnouncementAdminView, 0, len(rows))
	for _, row := range rows {
		views = append(views, &domain.AnnouncementAdminView{
			Announcement: row,
			Status:       announcementStatus(row, now),
		})
	}

	return views, total, nil
}

// Create adds an announcement. Super Admin only.
//
// The media is uploaded first and the record written second: a failed upload leaves nothing behind,
// whereas writing the row first would leave a banner pointing at a file that was never stored.
func (s *announcementService) Create(ctx context.Context, requesterRole, requesterID string, dto *domain.CreateAnnouncementDTO, media domain.MediaUpload) (*domain.Announcement, error) {
	if !isSuperAdmin(requesterRole) {
		return nil, fmt.Errorf("access denied: only a super admin can create announcements")
	}

	title := clampText(dto.Title, maxAnnouncementTitleLength)
	if title == "" {
		return nil, fmt.Errorf("give the banner a title")
	}

	audience, err := normalizeAudience(dto.Audience)
	if err != nil {
		return nil, err
	}
	if err := validateAnnouncementWindow(dto.StartsAt, dto.EndsAt); err != nil {
		return nil, err
	}
	linkURL, err := normalizeAnnouncementLink(dto.LinkURL)
	if err != nil {
		return nil, err
	}

	announcement := &domain.Announcement{
		Title:       title,
		Description: clampText(dto.Description, maxAnnouncementDescriptionLength),
		LinkURL:     linkURL,
		CTALabel:    clampText(dto.CTALabel, maxAnnouncementCTALength),
		Audience:    audience,
		IsActive:    dto.IsActive == nil || *dto.IsActive,
		StartsAt:    dto.StartsAt,
		EndsAt:      dto.EndsAt,
	}
	if dto.Priority != nil {
		announcement.Priority = *dto.Priority
	}

	if media.Present() {
		uploaded, err := s.storageSvc.UploadMedia(ctx, media.File, announcementMediaFolder)
		if err != nil {
			return nil, err
		}
		announcement.MediaURL = uploaded.SecureURL
		announcement.MediaPublicID = uploaded.PublicID
		announcement.MediaType = uploaded.ResourceType
		announcement.ThumbnailURL = uploaded.ThumbnailURL
	}

	if objID, err := bson.ObjectIDFromHex(requesterID); err == nil {
		announcement.CreatedByID = objID
		if creator, err := s.userRepo.FindByID(ctx, objID); err == nil && creator != nil {
			announcement.CreatedByName = creator.Name
		}
	}

	created, err := s.repo.Create(ctx, announcement)
	if err != nil {
		// The row didn't land, so the asset just uploaded belongs to nothing — remove it rather than
		// leaving it paid for and unreferenced.
		if announcement.MediaPublicID != "" {
			if delErr := s.storageSvc.DeleteMedia(ctx, announcement.MediaPublicID, announcement.MediaType); delErr != nil {
				log.Warn().Err(delErr).Msg("failed to clean up announcement media after a failed insert")
			}
		}
		return nil, err
	}

	return created, nil
}

// Update patches an announcement. Super Admin only.
func (s *announcementService) Update(ctx context.Context, requesterRole, idStr string, dto *domain.UpdateAnnouncementDTO, media domain.MediaUpload) (*domain.Announcement, error) {
	if !isSuperAdmin(requesterRole) {
		return nil, fmt.Errorf("access denied: only a super admin can update announcements")
	}

	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid announcement ID format: %w", err)
	}

	existing, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if dto.Title != nil {
		title := clampText(*dto.Title, maxAnnouncementTitleLength)
		if title == "" {
			return nil, fmt.Errorf("give the banner a title")
		}
		dto.Title = &title
	}
	if dto.Description != nil {
		description := clampText(*dto.Description, maxAnnouncementDescriptionLength)
		dto.Description = &description
	}
	if dto.CTALabel != nil {
		label := clampText(*dto.CTALabel, maxAnnouncementCTALength)
		dto.CTALabel = &label
	}
	if dto.Audience != nil {
		audience, err := normalizeAudience(*dto.Audience)
		if err != nil {
			return nil, err
		}
		dto.Audience = &audience
	}
	if dto.LinkURL != nil {
		link, err := normalizeAnnouncementLink(*dto.LinkURL)
		if err != nil {
			return nil, err
		}
		dto.LinkURL = &link
	}

	// Validated against what the record will actually hold once patched, not against the patch alone:
	// moving only the end date must still be checked against the existing start.
	start, end := existing.StartsAt, existing.EndsAt
	if dto.ClearStart {
		start = nil
	} else if dto.StartsAt != nil {
		start = dto.StartsAt
	}
	if dto.ClearEnd {
		end = nil
	} else if dto.EndsAt != nil {
		end = dto.EndsAt
	}
	if err := validateAnnouncementWindow(start, end); err != nil {
		return nil, err
	}

	// The old asset is only purged once the row points at the new one — the same order the product
	// brochure uses, so a failed write never leaves a banner referencing a deleted file.
	replacedPublicID, replacedType := "", ""
	if media.Present() {
		uploaded, err := s.storageSvc.UploadMedia(ctx, media.File, announcementMediaFolder)
		if err != nil {
			return nil, err
		}
		dto.MediaURL = &uploaded.SecureURL
		dto.MediaPublicID = &uploaded.PublicID
		dto.MediaType = &uploaded.ResourceType
		if uploaded.ThumbnailURL != "" {
			dto.ThumbnailURL = &uploaded.ThumbnailURL
		}
		dto.ClearMedia = false
		replacedPublicID, replacedType = existing.MediaPublicID, existing.MediaType
	} else if dto.ClearMedia {
		replacedPublicID, replacedType = existing.MediaPublicID, existing.MediaType
	}

	updated, err := s.repo.Update(ctx, id, dto)
	if err != nil {
		if media.Present() && dto.MediaPublicID != nil {
			if delErr := s.storageSvc.DeleteMedia(ctx, *dto.MediaPublicID, *dto.MediaType); delErr != nil {
				log.Warn().Err(delErr).Msg("failed to clean up replacement announcement media after a failed update")
			}
		}
		return nil, err
	}

	if replacedPublicID != "" {
		if delErr := s.storageSvc.DeleteMedia(ctx, replacedPublicID, replacedType); delErr != nil {
			log.Warn().Err(delErr).Str("public_id", replacedPublicID).Msg("failed to purge the replaced announcement media")
		}
	}

	return updated, nil
}

// Delete removes an announcement and purges its media. Super Admin only.
func (s *announcementService) Delete(ctx context.Context, requesterRole, idStr string) error {
	if !isSuperAdmin(requesterRole) {
		return fmt.Errorf("access denied: only a super admin can delete announcements")
	}

	id, err := bson.ObjectIDFromHex(idStr)
	if err != nil {
		return fmt.Errorf("invalid announcement ID format: %w", err)
	}

	existing, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	// The row goes first: if that fails nothing has changed and the request can be retried, whereas
	// purging the file first and then failing would leave a banner pointing at nothing.
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}

	if existing.MediaPublicID != "" {
		if delErr := s.storageSvc.DeleteMedia(ctx, existing.MediaPublicID, existing.MediaType); delErr != nil {
			log.Warn().Err(delErr).Str("public_id", existing.MediaPublicID).Msg("failed to purge announcement media")
		}
	}

	return nil
}

// announcementStatus is the live state of a banner, derived rather than stored so it can never
// disagree with the dates shown beside it.
func announcementStatus(a *domain.Announcement, now time.Time) string {
	switch {
	case !a.IsActive:
		return domain.AnnouncementStatusPaused
	case a.EndsAt != nil && a.EndsAt.Before(now):
		return domain.AnnouncementStatusExpired
	case a.StartsAt != nil && a.StartsAt.After(now):
		return domain.AnnouncementStatusScheduled
	default:
		return domain.AnnouncementStatusLive
	}
}

// normalizeAudience defaults a blank audience to everyone and refuses an unrecognised one.
//
// Refused rather than defaulted: silently widening "admins" to "all" because of a typo would put an
// internal notice on every client's screen.
func normalizeAudience(audience string) (string, error) {
	cleaned := strings.ToLower(strings.TrimSpace(audience))
	if cleaned == "" {
		return domain.AnnouncementAudienceAll, nil
	}
	if !domain.IsValidAnnouncementAudience(cleaned) {
		return "", fmt.Errorf("audience must be %q, %q or %q",
			domain.AnnouncementAudienceAll, domain.AnnouncementAudienceClients, domain.AnnouncementAudienceAdmins)
	}
	return cleaned, nil
}

// validateAnnouncementWindow refuses a window that could never show anything.
func validateAnnouncementWindow(startsAt, endsAt *time.Time) error {
	if startsAt != nil && endsAt != nil && !endsAt.After(*startsAt) {
		return fmt.Errorf("the end date must be after the start date")
	}
	return nil
}

// normalizeAnnouncementLink checks an optional tap-through.
//
// Only http(s) is allowed. Anything else is a scheme the app would hand to the operating system —
// which is how a banner becomes a way to open arbitrary deep links on someone's phone.
func normalizeAnnouncementLink(link string) (string, error) {
	cleaned := strings.TrimSpace(link)
	if cleaned == "" {
		return "", nil
	}
	lower := strings.ToLower(cleaned)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return "", fmt.Errorf("the link must start with http:// or https://")
	}
	return cleaned, nil
}

// clampText trims a field and caps it, counted in characters rather than bytes so a Bengali or Hindi
// title isn't cut to a third of its length or sliced mid-character.
func clampText(value string, max int) string {
	cleaned := strings.TrimSpace(value)
	if runes := []rune(cleaned); len(runes) > max {
		cleaned = strings.TrimSpace(string(runes[:max]))
	}
	return cleaned
}
