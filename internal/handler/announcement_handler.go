package handler

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"github.com/smart-invest-solutions/backend/internal/middleware"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// Upload limits for a banner. The image cap matches the rest of the app; video is allowed more room
// because even a few seconds of footage dwarfs a photo, but not so much that a client on mobile data
// pays for somebody's untrimmed clip.
const (
	maxAnnouncementImageSize = 10 << 20 // 10 MB
	maxAnnouncementVideoSize = 20 << 20 // 20 MB
)

var (
	allowedAnnouncementImageExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true}
	allowedAnnouncementVideoExt = map[string]bool{".mp4": true, ".mov": true, ".m4v": true, ".webm": true}
)

// AnnouncementHandler handles HTTP requests for screen banners.
type AnnouncementHandler struct {
	service domain.AnnouncementService
}

// NewAnnouncementHandler creates a new AnnouncementHandler.
func NewAnnouncementHandler(service domain.AnnouncementService) *AnnouncementHandler {
	return &AnnouncementHandler{service: service}
}

// GetMine returns the banners the caller should see right now.
// @Summary      Banners for me
// @Description  The announcements that belong on this caller's screen: active, inside their schedule window, and aimed at their audience. The audience is resolved from the caller's own role — a client cannot ask for the staff notices. Readable by every signed-in role.
// @Tags         Announcements
// @Produce      json
// @Success      200  {object}  response.APIResponse{data=[]domain.Announcement}  "Announcements retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Security     BearerAuth
// @Router       /announcements [get]
func (h *AnnouncementHandler) GetMine(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	announcements, err := h.service.GetForMe(c.Request.Context(), claims.Role)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, "Announcements retrieved successfully", announcements)
}

// GetAll returns every announcement for management. Super Admin only.
// @Summary      All banners (Super Admin only)
// @Description  Every announcement, newest first, each with a derived status — live, scheduled, expired or paused — so the list shows what is actually on screen without the reader comparing three fields.
// @Tags         Announcements
// @Produce      json
// @Param        page   query     int  false  "Page number (default 1)"
// @Param        limit  query     int  false  "Items per page (default 20, max 100)"
// @Success      200  {object}  response.PaginatedResponse{data=[]domain.AnnouncementAdminView}  "Announcements retrieved successfully"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — super_admin only"
// @Security     BearerAuth
// @Router       /announcements/all [get]
func (h *AnnouncementHandler) GetAll(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "20"), 10, 64)

	rows, total, err := h.service.GetAll(c.Request.Context(), claims.Role, page, limit)
	if err != nil {
		response.Error(c, statusForAccessError(err), err.Error())
		return
	}

	response.SuccessWithPagination(c, "Announcements retrieved successfully", rows, page, limit, total)
}

// Create adds a banner. Super Admin only.
// @Summary      Create a banner (Super Admin only)
// @Description  Creates an announcement from multipart form fields, with an optional image or video in the `media` field. `audience` is all/clients/admins (default all), `starts_at` and `ends_at` are optional RFC3339 bounds, `priority` orders several live banners (highest first), and `link_url` must be http(s) if given.
// @Tags         Announcements
// @Accept       multipart/form-data
// @Produce      json
// @Param        title        formData  string  true   "Banner title"
// @Param        description  formData  string  false  "Supporting line"
// @Param        media        formData  file    false  "Image (PNG/JPG/WEBP, max 10 MB) or video (MP4/MOV/WEBM, max 20 MB)"
// @Param        audience     formData  string  false  "Who sees it"  Enums(all, clients, admins)
// @Param        link_url     formData  string  false  "Optional http(s) tap-through"
// @Param        cta_label    formData  string  false  "Words on the tap-through button"
// @Param        is_active    formData  bool    false  "Published (default true)"
// @Param        starts_at    formData  string  false  "RFC3339 — when it goes live"
// @Param        ends_at      formData  string  false  "RFC3339 — when it comes down"
// @Param        priority     formData  int     false  "Higher shows first"
// @Success      201  {object}  response.APIResponse{data=domain.Announcement}  "Announcement created successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request — bad audience, window, link or media"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — super_admin only"
// @Security     BearerAuth
// @Router       /announcements [post]
func (h *AnnouncementHandler) Create(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	dto := &domain.CreateAnnouncementDTO{
		Title:       c.PostForm("title"),
		Description: c.PostForm("description"),
		LinkURL:     c.PostForm("link_url"),
		CTALabel:    c.PostForm("cta_label"),
		Audience:    c.PostForm("audience"),
	}
	if raw := c.PostForm("is_active"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			response.ValidationError(c, "is_active must be true or false")
			return
		}
		dto.IsActive = &parsed
	}
	if raw := c.PostForm("priority"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			response.ValidationError(c, "priority must be a whole number")
			return
		}
		dto.Priority = &parsed
	}
	startsAt, endsAt, err := announcementWindowFromForm(c)
	if err != nil {
		response.ValidationError(c, err.Error())
		return
	}
	dto.StartsAt, dto.EndsAt = startsAt, endsAt

	media, closeMedia, err := announcementMediaFromForm(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	defer closeMedia()

	created, err := h.service.Create(c.Request.Context(), claims.Role, claims.UserID.Hex(), dto, media)
	if err != nil {
		response.Error(c, statusForAccessError(err), err.Error())
		return
	}

	response.Created(c, "Announcement created successfully", created)
}

// Update patches a banner. Super Admin only.
// @Summary      Update a banner (Super Admin only)
// @Description  Patches an announcement — only the fields sent are changed. Uploading `media` replaces the existing file (the old one is purged once the record points at the new one); `clear_media=true` removes it, leaving a text-only banner. `clear_starts_at` / `clear_ends_at` drop a bound, since an omitted field means "leave alone".
// @Tags         Announcements
// @Accept       multipart/form-data
// @Produce      json
// @Param        id               path      string  true   "Announcement ID"
// @Param        title            formData  string  false  "Banner title"
// @Param        description      formData  string  false  "Supporting line"
// @Param        media            formData  file    false  "Replacement image or video"
// @Param        clear_media      formData  bool    false  "Remove the current image or video"
// @Param        audience         formData  string  false  "Who sees it"  Enums(all, clients, admins)
// @Param        link_url         formData  string  false  "Optional http(s) tap-through"
// @Param        cta_label        formData  string  false  "Words on the tap-through button"
// @Param        is_active        formData  bool    false  "Published"
// @Param        starts_at        formData  string  false  "RFC3339"
// @Param        ends_at          formData  string  false  "RFC3339"
// @Param        clear_starts_at  formData  bool    false  "Run from now"
// @Param        clear_ends_at    formData  bool    false  "Run until switched off"
// @Param        priority         formData  int     false  "Higher shows first"
// @Success      200  {object}  response.APIResponse{data=domain.Announcement}  "Announcement updated successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — super_admin only"
// @Security     BearerAuth
// @Router       /announcements/{id} [put]
func (h *AnnouncementHandler) Update(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	dto := &domain.UpdateAnnouncementDTO{}
	// Each field is only patched when the form actually carried it — `c.PostForm` can't tell "sent
	// empty" from "not sent", so GetPostForm is used to keep "clear the description" possible.
	if value, ok := c.GetPostForm("title"); ok {
		dto.Title = &value
	}
	if value, ok := c.GetPostForm("description"); ok {
		dto.Description = &value
	}
	if value, ok := c.GetPostForm("link_url"); ok {
		dto.LinkURL = &value
	}
	if value, ok := c.GetPostForm("cta_label"); ok {
		dto.CTALabel = &value
	}
	if value, ok := c.GetPostForm("audience"); ok {
		dto.Audience = &value
	}
	if value, ok := c.GetPostForm("is_active"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			response.ValidationError(c, "is_active must be true or false")
			return
		}
		dto.IsActive = &parsed
	}
	if value, ok := c.GetPostForm("priority"); ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			response.ValidationError(c, "priority must be a whole number")
			return
		}
		dto.Priority = &parsed
	}

	startsAt, endsAt, err := announcementWindowFromForm(c)
	if err != nil {
		response.ValidationError(c, err.Error())
		return
	}
	dto.StartsAt, dto.EndsAt = startsAt, endsAt
	dto.ClearStart = announcementFlag(c, "clear_starts_at")
	dto.ClearEnd = announcementFlag(c, "clear_ends_at")
	dto.ClearMedia = announcementFlag(c, "clear_media")

	media, closeMedia, err := announcementMediaFromForm(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	defer closeMedia()

	updated, err := h.service.Update(c.Request.Context(), claims.Role, c.Param("id"), dto, media)
	if err != nil {
		response.Error(c, statusForAccessError(err), err.Error())
		return
	}

	response.Success(c, "Announcement updated successfully", updated)
}

// Delete removes a banner and its media. Super Admin only.
// @Summary      Delete a banner (Super Admin only)
// @Description  Removes the announcement and purges its image or video from storage.
// @Tags         Announcements
// @Produce      json
// @Param        id  path  string  true  "Announcement ID"
// @Success      200  {object}  response.APIResponse  "Announcement deleted successfully"
// @Failure      400  {object}  response.APIResponse  "Bad request"
// @Failure      401  {object}  response.APIResponse  "Unauthorized"
// @Failure      403  {object}  response.APIResponse  "Forbidden — super_admin only"
// @Security     BearerAuth
// @Router       /announcements/{id} [delete]
func (h *AnnouncementHandler) Delete(c *gin.Context) {
	claims, ok := middleware.GetClaims(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.service.Delete(c.Request.Context(), claims.Role, c.Param("id")); err != nil {
		response.Error(c, statusForAccessError(err), err.Error())
		return
	}

	response.Success(c, "Announcement deleted successfully", nil)
}

// announcementFlag reads an optional boolean form field, treating anything unparseable as false — a
// malformed flag means "don't do the destructive thing", which is the safe reading.
func announcementFlag(c *gin.Context, field string) bool {
	parsed, err := strconv.ParseBool(c.PostForm(field))
	return err == nil && parsed
}

// announcementWindowFromForm parses the optional schedule bounds.
func announcementWindowFromForm(c *gin.Context) (*time.Time, *time.Time, error) {
	parse := func(field string) (*time.Time, error) {
		raw := strings.TrimSpace(c.PostForm(field))
		if raw == "" {
			return nil, nil
		}
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, fmt.Errorf("%s must be an RFC3339 date", field)
		}
		utc := parsed.UTC()
		return &utc, nil
	}

	startsAt, err := parse("starts_at")
	if err != nil {
		return nil, nil, err
	}
	endsAt, err := parse("ends_at")
	if err != nil {
		return nil, nil, err
	}
	return startsAt, endsAt, nil
}

// announcementMediaFromForm reads and validates the optional media file.
//
// Returns a closer rather than closing here: the stream has to stay open until the service has
// uploaded it, so the caller defers this.
func announcementMediaFromForm(c *gin.Context) (domain.MediaUpload, func(), error) {
	noop := func() {}

	fileHeader, err := c.FormFile("media")
	if err != nil || fileHeader == nil {
		return domain.MediaUpload{}, noop, nil
	}

	if err := validateAnnouncementMedia(fileHeader); err != nil {
		return domain.MediaUpload{}, noop, err
	}

	stream, err := fileHeader.Open()
	if err != nil {
		return domain.MediaUpload{}, noop, fmt.Errorf("failed to read the uploaded file")
	}

	return domain.MediaUpload{File: stream, Filename: fileHeader.Filename}, func() { _ = stream.Close() }, nil
}

// validateAnnouncementMedia checks the file before anything is uploaded.
//
// The extension decides which size cap and which content types apply — but it is only the client's
// claim, so the bytes are sniffed too: a video renamed .jpg would otherwise slip past the image cap
// and be filed as an image, which is also the state in which it could never be deleted.
func validateAnnouncementMedia(fileHeader *multipart.FileHeader) error {
	if fileHeader.Size <= 0 {
		return fmt.Errorf("the uploaded file is empty")
	}

	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	isImage := allowedAnnouncementImageExt[ext]
	isVideo := allowedAnnouncementVideoExt[ext]
	if !isImage && !isVideo {
		return fmt.Errorf("unsupported file type — use PNG, JPG or WEBP for an image, or MP4, MOV or WEBM for a video")
	}

	limit := int64(maxAnnouncementImageSize)
	if isVideo {
		limit = maxAnnouncementVideoSize
	}
	if fileHeader.Size > limit {
		return fmt.Errorf("that file is %.1f MB — the limit is %d MB", float64(fileHeader.Size)/(1<<20), limit>>20)
	}

	f, err := fileHeader.Open()
	if err != nil {
		return fmt.Errorf("failed to read the uploaded file")
	}
	defer f.Close()

	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	detected := http.DetectContentType(head[:n])
	switch {
	case isImage && strings.HasPrefix(detected, "image/"):
		return nil
	case isVideo && strings.HasPrefix(detected, "video/"):
		return nil
	default:
		return fmt.Errorf("that file's contents don't match its name (detected %s)", detected)
	}
}
