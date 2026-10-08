package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

/* ---------------------------------------------------------------- fakes */

type fakeAnnouncementRepo struct {
	domain.AnnouncementRepository
	rows map[bson.ObjectID]*domain.Announcement
	// What FindLive was asked for, so the tests can assert the audience the service resolved.
	askedAudiences []string
	askedNow       time.Time
	live           []*domain.Announcement
	createErr      error
	updateErr      error
	deleted        []bson.ObjectID
}

func newFakeAnnouncementRepo(rows ...*domain.Announcement) *fakeAnnouncementRepo {
	r := &fakeAnnouncementRepo{rows: map[bson.ObjectID]*domain.Announcement{}}
	for _, row := range rows {
		if row.ID.IsZero() {
			row.ID = bson.NewObjectID()
		}
		r.rows[row.ID] = row
	}
	return r
}

func (r *fakeAnnouncementRepo) Create(_ context.Context, a *domain.Announcement) (*domain.Announcement, error) {
	if r.createErr != nil {
		return nil, r.createErr
	}
	a.ID = bson.NewObjectID()
	r.rows[a.ID] = a
	return a, nil
}

func (r *fakeAnnouncementRepo) FindByID(_ context.Context, id bson.ObjectID) (*domain.Announcement, error) {
	if row, ok := r.rows[id]; ok {
		return row, nil
	}
	return nil, errors.New("announcement not found")
}

func (r *fakeAnnouncementRepo) FindLive(_ context.Context, audiences []string, now time.Time) ([]*domain.Announcement, error) {
	r.askedAudiences, r.askedNow = audiences, now
	return r.live, nil
}

func (r *fakeAnnouncementRepo) FindAll(_ context.Context, _, _ int64) ([]*domain.Announcement, int64, error) {
	rows := make([]*domain.Announcement, 0, len(r.rows))
	for _, row := range r.rows {
		rows = append(rows, row)
	}
	return rows, int64(len(rows)), nil
}

func (r *fakeAnnouncementRepo) Update(_ context.Context, id bson.ObjectID, dto *domain.UpdateAnnouncementDTO) (*domain.Announcement, error) {
	if r.updateErr != nil {
		return nil, r.updateErr
	}
	row, ok := r.rows[id]
	if !ok {
		return nil, errors.New("announcement not found")
	}
	if dto.MediaURL != nil {
		row.MediaURL = *dto.MediaURL
	}
	if dto.MediaPublicID != nil {
		row.MediaPublicID = *dto.MediaPublicID
	}
	if dto.MediaType != nil {
		row.MediaType = *dto.MediaType
	}
	if dto.ClearMedia {
		row.MediaURL, row.MediaPublicID, row.MediaType, row.ThumbnailURL = "", "", "", ""
	}
	if dto.Title != nil {
		row.Title = *dto.Title
	}
	if dto.Audience != nil {
		row.Audience = *dto.Audience
	}
	return row, nil
}

func (r *fakeAnnouncementRepo) Delete(_ context.Context, id bson.ObjectID) error {
	if _, ok := r.rows[id]; !ok {
		return errors.New("announcement not found")
	}
	delete(r.rows, id)
	r.deleted = append(r.deleted, id)
	return nil
}

// fakeStorage records what it was asked to upload and delete, so the media lifecycle can be checked
// without a storage provider.
type fakeStorage struct {
	StorageService
	uploaded    int
	uploadErr   error
	resource    string
	purged      []string
	purgedTypes []string
}

func (s *fakeStorage) UploadMedia(_ context.Context, file io.Reader, _ string) (*UploadResult, error) {
	if s.uploadErr != nil {
		return nil, s.uploadErr
	}
	_, _ = io.ReadAll(file)
	s.uploaded++
	resource := s.resource
	if resource == "" {
		resource = ResourceTypeImage
	}
	result := &UploadResult{
		SecureURL:    "https://cdn.example.com/banner." + resource,
		PublicID:     "banners/" + resource,
		ResourceType: resource,
	}
	if resource == ResourceTypeVideo {
		result.ThumbnailURL = "https://cdn.example.com/banner.jpg"
	}
	return result, nil
}

func (s *fakeStorage) DeleteMedia(_ context.Context, publicID, resourceType string) error {
	s.purged = append(s.purged, publicID)
	s.purgedTypes = append(s.purgedTypes, resourceType)
	return nil
}

func newAnnouncementService(t *testing.T, repo *fakeAnnouncementRepo, storage *fakeStorage) (domain.AnnouncementService, *domain.User) {
	t.Helper()
	root := &domain.User{ID: bson.NewObjectID(), Name: "Head Office", Role: domain.RoleSuperAdmin, IsActive: true}
	return NewAnnouncementService(repo, newFakeUserRepo(root), storage), root
}

func mediaFrom(body string) domain.MediaUpload {
	return domain.MediaUpload{File: strings.NewReader(body), Filename: "banner.png"}
}

/* ---------------------------------------------------------------- tests */

func TestTheAudienceIsResolvedFromTheCallersRole(t *testing.T) {
	// A client asking for the staff notices must get the clients' ones: the request never says who
	// the caller is, their token does.
	repo := newFakeAnnouncementRepo()
	svc, _ := newAnnouncementService(t, repo, &fakeStorage{})
	ctx := context.Background()

	cases := []struct {
		role string
		want string
	}{
		{domain.RoleClient, domain.AnnouncementAudienceClients},
		{domain.RoleAdvisor, domain.AnnouncementAudienceClients},
		{domain.RoleAdmin, domain.AnnouncementAudienceAdmins},
		{domain.RoleSuperAdmin, domain.AnnouncementAudienceAdmins},
	}

	for _, tc := range cases {
		if _, err := svc.GetForMe(ctx, tc.role); err != nil {
			t.Fatalf("GetForMe(%s): %v", tc.role, err)
		}
		asked := strings.Join(repo.askedAudiences, ",")
		// "all" always comes along — a platform-wide banner is for everybody.
		if !strings.Contains(asked, domain.AnnouncementAudienceAll) {
			t.Errorf("%s: expected the shared audience to be included, got %v", tc.role, repo.askedAudiences)
		}
		if !strings.Contains(asked, tc.want) {
			t.Errorf("%s: expected audience %q, got %v", tc.role, tc.want, repo.askedAudiences)
		}
		for _, unwanted := range []string{domain.AnnouncementAudienceClients, domain.AnnouncementAudienceAdmins} {
			if unwanted != tc.want && strings.Contains(asked, unwanted) {
				t.Errorf("%s: must not be shown the %q audience", tc.role, unwanted)
			}
		}
	}
}

func TestAnnouncementStatusIsDerivedFromTheFlagAndTheWindow(t *testing.T) {
	now := time.Now().UTC()
	past, future := now.Add(-48*time.Hour), now.Add(48*time.Hour)

	cases := []struct {
		name string
		row  *domain.Announcement
		want string
	}{
		{name: "no bounds and published", row: &domain.Announcement{IsActive: true}, want: domain.AnnouncementStatusLive},
		{name: "switched off", row: &domain.Announcement{IsActive: false}, want: domain.AnnouncementStatusPaused},
		{name: "switched off beats an expired window", row: &domain.Announcement{IsActive: false, EndsAt: &past}, want: domain.AnnouncementStatusPaused},
		{name: "window still to open", row: &domain.Announcement{IsActive: true, StartsAt: &future}, want: domain.AnnouncementStatusScheduled},
		{name: "window closed", row: &domain.Announcement{IsActive: true, EndsAt: &past}, want: domain.AnnouncementStatusExpired},
		{name: "inside the window", row: &domain.Announcement{IsActive: true, StartsAt: &past, EndsAt: &future}, want: domain.AnnouncementStatusLive},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := announcementStatus(tc.row, now); got != tc.want {
				t.Errorf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOnlyASuperAdminCanCurateBanners(t *testing.T) {
	// A banner goes on every screen on the platform, so one agency's admin must not be able to put
	// something there.
	repo := newFakeAnnouncementRepo(&domain.Announcement{Title: "Existing", IsActive: true})
	storage := &fakeStorage{}
	svc, _ := newAnnouncementService(t, repo, storage)
	ctx := context.Background()
	var existingID bson.ObjectID
	for id := range repo.rows {
		existingID = id
	}

	for _, role := range []string{domain.RoleAdmin, domain.RoleClient, domain.RoleAdvisor} {
		if _, err := svc.Create(ctx, role, bson.NewObjectID().Hex(),
			&domain.CreateAnnouncementDTO{Title: "Offer"}, domain.MediaUpload{}); err == nil {
			t.Errorf("%s must not be able to create a banner", role)
		}
		if _, err := svc.Update(ctx, role, existingID.Hex(), &domain.UpdateAnnouncementDTO{}, domain.MediaUpload{}); err == nil {
			t.Errorf("%s must not be able to update a banner", role)
		}
		if err := svc.Delete(ctx, role, existingID.Hex()); err == nil {
			t.Errorf("%s must not be able to delete a banner", role)
		}
		if _, _, err := svc.GetAll(ctx, role, 1, 20); err == nil {
			t.Errorf("%s must not be able to list every banner", role)
		}
	}
	if storage.uploaded != 0 || len(repo.deleted) != 0 {
		t.Error("a refused request must touch neither storage nor the record")
	}
}

func TestCreatingABannerStoresItsMediaAndResourceType(t *testing.T) {
	// The resource type is what makes the asset deletable later, so it has to be persisted — not
	// re-guessed at delete time.
	repo := newFakeAnnouncementRepo()
	storage := &fakeStorage{resource: ResourceTypeVideo}
	svc, root := newAnnouncementService(t, repo, storage)

	created, err := svc.Create(context.Background(), domain.RoleSuperAdmin, root.ID.Hex(),
		&domain.CreateAnnouncementDTO{Title: "Diwali offer", Description: "Renew and save"},
		mediaFrom("fake-video-bytes"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.MediaType != ResourceTypeVideo {
		t.Errorf("media type = %q, want %q", created.MediaType, ResourceTypeVideo)
	}
	if created.ThumbnailURL == "" {
		t.Error("a video banner needs a still to show before anyone presses play")
	}
	if created.Audience != domain.AnnouncementAudienceAll {
		t.Errorf("audience should default to everyone, got %q", created.Audience)
	}
	if !created.IsActive {
		t.Error("filling in the form means publishing — it should default to active")
	}
	if created.CreatedByName != "Head Office" {
		t.Errorf("the creator should be recorded, got %q", created.CreatedByName)
	}
}

func TestAFailedInsertDoesNotLeaveTheUploadBehind(t *testing.T) {
	// The file is uploaded before the row exists, so a failed write has to clean up after itself or
	// the storage bill grows with every failure.
	repo := newFakeAnnouncementRepo()
	repo.createErr = errors.New("database unavailable")
	storage := &fakeStorage{}
	svc, root := newAnnouncementService(t, repo, storage)

	if _, err := svc.Create(context.Background(), domain.RoleSuperAdmin, root.ID.Hex(),
		&domain.CreateAnnouncementDTO{Title: "Offer"}, mediaFrom("bytes")); err == nil {
		t.Fatal("expected the insert failure to surface")
	}
	if len(storage.purged) != 1 {
		t.Fatalf("the orphaned upload should have been removed, purged %v", storage.purged)
	}
}

func TestReplacingTheMediaPurgesTheOldFileWithItsOwnType(t *testing.T) {
	// A video replaced by an image must be deleted as a video. Sent as an image, the provider accepts
	// it and does nothing — the file stays forever, and the bug is invisible.
	existing := &domain.Announcement{
		Title: "Old", IsActive: true,
		MediaURL: "https://cdn.example.com/old.mp4", MediaPublicID: "banners/old", MediaType: ResourceTypeVideo,
	}
	repo := newFakeAnnouncementRepo(existing)
	storage := &fakeStorage{resource: ResourceTypeImage}
	svc, _ := newAnnouncementService(t, repo, storage)

	if _, err := svc.Update(context.Background(), domain.RoleSuperAdmin, existing.ID.Hex(),
		&domain.UpdateAnnouncementDTO{}, mediaFrom("new-image")); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(storage.purged) != 1 || storage.purged[0] != "banners/old" {
		t.Fatalf("the old asset should have been purged, got %v", storage.purged)
	}
	if storage.purgedTypes[0] != ResourceTypeVideo {
		t.Errorf("purged as %q, want %q — the stored type, not the new one", storage.purgedTypes[0], ResourceTypeVideo)
	}
}

func TestClearingTheMediaRemovesTheFileToo(t *testing.T) {
	existing := &domain.Announcement{
		Title: "Old", IsActive: true,
		MediaPublicID: "banners/pic", MediaType: ResourceTypeImage,
	}
	repo := newFakeAnnouncementRepo(existing)
	storage := &fakeStorage{}
	svc, _ := newAnnouncementService(t, repo, storage)

	updated, err := svc.Update(context.Background(), domain.RoleSuperAdmin, existing.ID.Hex(),
		&domain.UpdateAnnouncementDTO{ClearMedia: true}, domain.MediaUpload{})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.MediaURL != "" || updated.MediaPublicID != "" {
		t.Errorf("the record should no longer reference media: %+v", updated)
	}
	if len(storage.purged) != 1 || storage.purgedTypes[0] != ResourceTypeImage {
		t.Errorf("the file should have been purged as an image, got %v / %v", storage.purged, storage.purgedTypes)
	}
}

func TestDeletingABannerPurgesItsMedia(t *testing.T) {
	existing := &domain.Announcement{
		Title: "Old", IsActive: true,
		MediaPublicID: "banners/clip", MediaType: ResourceTypeVideo,
	}
	repo := newFakeAnnouncementRepo(existing)
	storage := &fakeStorage{}
	svc, _ := newAnnouncementService(t, repo, storage)

	if err := svc.Delete(context.Background(), domain.RoleSuperAdmin, existing.ID.Hex()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(repo.deleted) != 1 {
		t.Error("the record should be gone")
	}
	if len(storage.purged) != 1 || storage.purgedTypes[0] != ResourceTypeVideo {
		t.Errorf("the video should have been purged as a video, got %v / %v", storage.purged, storage.purgedTypes)
	}
}

func TestAnUnrecognisedAudienceIsRefusedRatherThanWidened(t *testing.T) {
	// Defaulting a typo to "all" would put an internal staff notice on every client's screen.
	repo := newFakeAnnouncementRepo()
	svc, root := newAnnouncementService(t, repo, &fakeStorage{})

	_, err := svc.Create(context.Background(), domain.RoleSuperAdmin, root.ID.Hex(),
		&domain.CreateAnnouncementDTO{Title: "Internal", Audience: "admin"}, domain.MediaUpload{})
	if err == nil {
		t.Fatal("expected an unrecognised audience to be refused")
	}
	if !strings.Contains(err.Error(), "audience must be") {
		t.Errorf("the error should name the valid values, got %v", err)
	}

	// The real values are accepted, whatever case they arrive in.
	for _, audience := range []string{"ALL", " clients ", "Admins"} {
		created, err := svc.Create(context.Background(), domain.RoleSuperAdmin, root.ID.Hex(),
			&domain.CreateAnnouncementDTO{Title: "Offer", Audience: audience}, domain.MediaUpload{})
		if err != nil {
			t.Fatalf("audience %q: %v", audience, err)
		}
		if !domain.IsValidAnnouncementAudience(created.Audience) {
			t.Errorf("audience %q stored as %q", audience, created.Audience)
		}
	}
}

func TestAWindowThatCouldNeverShowAnythingIsRefused(t *testing.T) {
	repo := newFakeAnnouncementRepo()
	svc, root := newAnnouncementService(t, repo, &fakeStorage{})
	ctx := context.Background()

	now := time.Now().UTC()
	start, end := now.Add(48*time.Hour), now.Add(24*time.Hour)
	_, err := svc.Create(ctx, domain.RoleSuperAdmin, root.ID.Hex(),
		&domain.CreateAnnouncementDTO{Title: "Backwards", StartsAt: &start, EndsAt: &end}, domain.MediaUpload{})
	if err == nil || !strings.Contains(err.Error(), "end date must be after") {
		t.Fatalf("expected a backwards window to be refused, got %v", err)
	}

	// And on update, checked against what the record will actually hold: moving only the end date has
	// to be compared with the existing start, not with nothing.
	existing := &domain.Announcement{Title: "Live", IsActive: true, StartsAt: &start}
	repo.rows[bson.NewObjectID()] = existing
	for id, row := range repo.rows {
		if row == existing {
			existing.ID = id
		}
	}
	if _, err := svc.Update(ctx, domain.RoleSuperAdmin, existing.ID.Hex(),
		&domain.UpdateAnnouncementDTO{EndsAt: &end}, domain.MediaUpload{}); err == nil {
		t.Error("an end date before the stored start must be refused")
	}
}

func TestAOneDayBannerIsValid(t *testing.T) {
	// The app sends the start at midday and the end at end-of-day, so a banner that starts and ends on
	// the same date is a real one-day offer — not a backwards window. Before the end bound was sent as
	// end-of-day, both were midday and this was refused.
	repo := newFakeAnnouncementRepo()
	svc, root := newAnnouncementService(t, repo, &fakeStorage{})

	day := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	start := day.Add(12 * time.Hour)
	end := day.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	created, err := svc.Create(context.Background(), domain.RoleSuperAdmin, root.ID.Hex(),
		&domain.CreateAnnouncementDTO{Title: "One-day offer", StartsAt: &start, EndsAt: &end},
		domain.MediaUpload{})
	if err != nil {
		t.Fatalf("a same-day banner should be accepted: %v", err)
	}
	if created.StartsAt == nil || created.EndsAt == nil {
		t.Fatal("both bounds should be stored")
	}

	// And it is live for the whole of that day, including the evening — the thing the end-of-day
	// bound exists to guarantee.
	evening := day.Add(21 * time.Hour)
	if got := announcementStatus(created, evening); got != domain.AnnouncementStatusLive {
		t.Errorf("at 21:00 on its only day the banner reads %q, want live", got)
	}
	// The following midnight is past it.
	if got := announcementStatus(created, day.Add(25*time.Hour)); got != domain.AnnouncementStatusExpired {
		t.Errorf("the next day it reads %q, want expired", got)
	}
}

func TestOnlyHttpLinksAreAccepted(t *testing.T) {
	// A banner that can carry any scheme is a way to make somebody's phone open an arbitrary deep
	// link. Only the two web schemes are allowed through.
	repo := newFakeAnnouncementRepo()
	svc, root := newAnnouncementService(t, repo, &fakeStorage{})
	ctx := context.Background()

	for _, link := range []string{"javascript:alert(1)", "tel:+919876500000", "myapp://pay", "ftp://files"} {
		if _, err := svc.Create(ctx, domain.RoleSuperAdmin, root.ID.Hex(),
			&domain.CreateAnnouncementDTO{Title: "Offer", LinkURL: link}, domain.MediaUpload{}); err == nil {
			t.Errorf("link %q should have been refused", link)
		}
	}
	for _, link := range []string{"https://ulmind.com/offer", "http://ulmind.com", " https://ulmind.com "} {
		if _, err := svc.Create(ctx, domain.RoleSuperAdmin, root.ID.Hex(),
			&domain.CreateAnnouncementDTO{Title: "Offer", LinkURL: link}, domain.MediaUpload{}); err != nil {
			t.Errorf("link %q should have been accepted: %v", link, err)
		}
	}
}

func TestBannerTextIsTrimmedAndCapped(t *testing.T) {
	repo := newFakeAnnouncementRepo()
	svc, root := newAnnouncementService(t, repo, &fakeStorage{})

	created, err := svc.Create(context.Background(), domain.RoleSuperAdmin, root.ID.Hex(),
		&domain.CreateAnnouncementDTO{
			Title:       "  " + strings.Repeat("A", 200) + "  ",
			Description: strings.Repeat("B", 500),
			CTALabel:    strings.Repeat("C", 100),
		}, domain.MediaUpload{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len([]rune(created.Title)) != maxAnnouncementTitleLength {
		t.Errorf("title is %d chars, want %d", len([]rune(created.Title)), maxAnnouncementTitleLength)
	}
	if len([]rune(created.Description)) != maxAnnouncementDescriptionLength {
		t.Errorf("description is %d chars, want %d", len([]rune(created.Description)), maxAnnouncementDescriptionLength)
	}
	if len([]rune(created.CTALabel)) != maxAnnouncementCTALength {
		t.Errorf("CTA is %d chars, want %d", len([]rune(created.CTALabel)), maxAnnouncementCTALength)
	}

	// A blank title is the one field a banner can't do without.
	if _, err := svc.Create(context.Background(), domain.RoleSuperAdmin, root.ID.Hex(),
		&domain.CreateAnnouncementDTO{Title: "   "}, domain.MediaUpload{}); err == nil {
		t.Error("a banner with no title should be refused")
	}
}

func TestATitleInBengaliIsCappedByCharactersNotBytes(t *testing.T) {
	repo := newFakeAnnouncementRepo()
	svc, root := newAnnouncementService(t, repo, &fakeStorage{})

	created, err := svc.Create(context.Background(), domain.RoleSuperAdmin, root.ID.Hex(),
		&domain.CreateAnnouncementDTO{Title: strings.Repeat("শু", 200)}, domain.MediaUpload{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if runes := []rune(created.Title); len(runes) != maxAnnouncementTitleLength {
		t.Errorf("got %d characters, want %d", len(runes), maxAnnouncementTitleLength)
	}
	if strings.Contains(created.Title, "�") {
		t.Error("the title was cut mid-character")
	}
}

func TestAFailedUploadLeavesTheExistingBannerAlone(t *testing.T) {
	existing := &domain.Announcement{
		Title: "Live offer", IsActive: true,
		MediaPublicID: "banners/current", MediaType: ResourceTypeImage,
	}
	repo := newFakeAnnouncementRepo(existing)
	storage := &fakeStorage{uploadErr: errors.New("storage unavailable")}
	svc, _ := newAnnouncementService(t, repo, storage)

	if _, err := svc.Update(context.Background(), domain.RoleSuperAdmin, existing.ID.Hex(),
		&domain.UpdateAnnouncementDTO{}, mediaFrom("bytes")); err == nil {
		t.Fatal("expected the upload failure to surface")
	}
	if len(storage.purged) != 0 {
		t.Error("the current banner's file must survive a failed replacement")
	}
	if existing.MediaPublicID != "banners/current" {
		t.Errorf("the record should be untouched, got %q", existing.MediaPublicID)
	}
}
