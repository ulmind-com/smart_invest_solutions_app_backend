package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

/* ---------------------------------------------------------------- fakes */

// Extra fakeUserRepo methods the referral flows need (the type itself lives in fixes_test.go).

func (r *fakeUserRepo) Create(_ context.Context, user *domain.User) (*domain.User, error) {
	if user.ID.IsZero() {
		user.ID = bson.NewObjectID()
	}
	user.CreatedAt = time.Now().UTC()
	r.users[user.ID] = user
	return user, nil
}

func (r *fakeUserRepo) UpdatePIN(_ context.Context, id bson.ObjectID, hashedPIN string) error {
	r.users[id].PIN = hashedPIN
	return nil
}

func (r *fakeUserRepo) FindAllByRoles(_ context.Context, roles []string, page, limit int64) ([]*domain.User, int64, error) {
	wanted := map[string]bool{}
	for _, role := range roles {
		wanted[role] = true
	}
	var matched []*domain.User
	for _, u := range r.users {
		if wanted[u.Role] {
			matched = append(matched, u)
		}
	}
	// Stable order so paging is deterministic.
	for i := 1; i < len(matched); i++ {
		for j := i; j > 0 && matched[j].Email < matched[j-1].Email; j-- {
			matched[j], matched[j-1] = matched[j-1], matched[j]
		}
	}
	total := int64(len(matched))
	start := (page - 1) * limit
	if start >= total {
		return nil, total, nil
	}
	end := start + limit
	if end > total {
		end = total
	}
	return matched[start:end], total, nil
}

type fakeReferralRepo struct {
	records []*domain.ReferralRecord
}

func (r *fakeReferralRepo) Create(_ context.Context, record *domain.ReferralRecord) (*domain.ReferralRecord, error) {
	record.ID = bson.NewObjectID()
	record.ReferredEmail = strings.ToLower(strings.TrimSpace(record.ReferredEmail))
	if record.Status == "" {
		record.Status = domain.ReferralStatusPending
	}
	r.records = append(r.records, record)
	return record, nil
}

func (r *fakeReferralRepo) GetPendingByReferredEmail(_ context.Context, email string) (*domain.ReferralRecord, error) {
	for _, rec := range r.records {
		if strings.EqualFold(rec.ReferredEmail, strings.TrimSpace(email)) && rec.Status == domain.ReferralStatusPending {
			return rec, nil
		}
	}
	return nil, nil
}

func (r *fakeReferralRepo) Complete(_ context.Context, id, referredUserID bson.ObjectID, referredName, referredPhone string) error {
	for _, rec := range r.records {
		if rec.ID == id && rec.Status == domain.ReferralStatusPending {
			now := time.Now().UTC()
			rec.Status = domain.ReferralStatusCompleted
			rec.ReferredUserID = &referredUserID
			rec.CompletedAt = &now
			if referredName != "" {
				rec.ReferredName = referredName
			}
			if referredPhone != "" {
				rec.ReferredPhone = referredPhone
			}
		}
	}
	return nil
}

func (r *fakeReferralRepo) CountsByReferrerID(_ context.Context, referrerID bson.ObjectID) (domain.ReferralCounts, error) {
	var counts domain.ReferralCounts
	for _, rec := range r.records {
		if rec.ReferrerID != referrerID {
			continue
		}
		if rec.Status == domain.ReferralStatusCompleted {
			counts.Completed++
		} else {
			counts.Pending++
		}
	}
	return counts, nil
}

func (r *fakeReferralRepo) CountsByReferrer(_ context.Context) (map[bson.ObjectID]domain.ReferralCounts, error) {
	out := map[bson.ObjectID]domain.ReferralCounts{}
	for _, rec := range r.records {
		entry := out[rec.ReferrerID]
		if rec.Status == domain.ReferralStatusCompleted {
			entry.Completed++
		} else {
			entry.Pending++
		}
		out[rec.ReferrerID] = entry
	}
	return out, nil
}

func (r *fakeReferralRepo) GetAll(_ context.Context, page, limit int64, referrerID *bson.ObjectID) ([]*domain.ReferralRecordWithDetails, int64, error) {
	var rows []*domain.ReferralRecordWithDetails
	for _, rec := range r.records {
		if referrerID != nil && rec.ReferrerID != *referrerID {
			continue
		}
		rows = append(rows, &domain.ReferralRecordWithDetails{
			ID:            rec.ID,
			ReferrerID:    rec.ReferrerID,
			ReferredEmail: rec.ReferredEmail,
			ReferredName:  rec.ReferredName,
			Status:        rec.Status,
		})
	}
	return rows, int64(len(rows)), nil
}

func (r *fakeReferralRepo) ReassignReferrer(_ context.Context, fromUserID, toUserID bson.ObjectID) (int64, error) {
	var moved int64
	for _, rec := range r.records {
		if rec.ReferrerID == fromUserID {
			rec.ReferrerID = toUserID
			moved++
		}
	}
	return moved, nil
}

type fakeAccessReqRepo struct {
	domain.AccessRequestRepository
	requests map[bson.ObjectID]*domain.AccessRequest
}

func newFakeAccessReqRepo(reqs ...*domain.AccessRequest) *fakeAccessReqRepo {
	r := &fakeAccessReqRepo{requests: map[bson.ObjectID]*domain.AccessRequest{}}
	for _, req := range reqs {
		if req.ID.IsZero() {
			req.ID = bson.NewObjectID()
		}
		if req.Status == "" {
			req.Status = domain.AccessStatusPending
		}
		r.requests[req.ID] = req
	}
	return r
}

func (r *fakeAccessReqRepo) FindByID(_ context.Context, id bson.ObjectID) (*domain.AccessRequest, error) {
	if req, ok := r.requests[id]; ok {
		return req, nil
	}
	return nil, domain.ErrUserNotFound
}

func (r *fakeAccessReqRepo) FindByEmail(_ context.Context, email string) (*domain.AccessRequest, error) {
	for _, req := range r.requests {
		if strings.EqualFold(req.Email, strings.TrimSpace(email)) {
			return req, nil
		}
	}
	return nil, nil
}

func (r *fakeAccessReqRepo) Create(_ context.Context, req *domain.AccessRequest) (*domain.AccessRequest, error) {
	req.ID = bson.NewObjectID()
	r.requests[req.ID] = req
	return req, nil
}

func (r *fakeAccessReqRepo) ClaimApproval(_ context.Context, id bson.ObjectID, adminNotes string) (*domain.AccessRequest, error) {
	req := r.requests[id]
	req.Status = domain.AccessStatusApproved
	req.AdminNotes = adminNotes
	return req, nil
}

func (r *fakeAccessReqRepo) RevertApproval(_ context.Context, id bson.ObjectID) error {
	r.requests[id].Status = domain.AccessStatusPending
	return nil
}

/* ---------------------------------------------------------------- tests */

func staffAdmin(adminID, code string) *domain.User {
	return &domain.User{
		ID:           bson.NewObjectID(),
		Name:         "Admin " + adminID,
		Email:        strings.ToLower(adminID) + "@agency.in",
		Role:         domain.RoleAdmin,
		IsActive:     true,
		AdminID:      adminID,
		ReferralCode: code,
	}
}

// A referral code issued to an admin also decides the agency, so an admin only has to share one code.
func TestResolveOnboardingAgencyUsesReferralCode(t *testing.T) {
	admin := staffAdmin("ADM-AAAAAA", "AB12CD")
	client := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleClient, Email: "c@x.in", ReferralCode: "LEGACY"}
	repo := newFakeUserRepo(admin, client)
	ctx := context.Background()

	got, err := resolveOnboardingAgency(ctx, repo, "", "ab12cd")
	if err != nil || got != "ADM-AAAAAA" {
		t.Fatalf("referral code should resolve to the admin's agency, got %q (%v)", got, err)
	}

	// A legacy client code refers no one and decides no agency.
	got, err = resolveOnboardingAgency(ctx, repo, "", "LEGACY")
	if err != nil || got != "" {
		t.Fatalf("a client's code must not set an agency, got %q (%v)", got, err)
	}

	// An explicitly typed Agency ID always wins.
	other := staffAdmin("ADM-BBBBBB", "ZZ99ZZ")
	repo.users[other.ID] = other
	got, err = resolveOnboardingAgency(ctx, repo, "adm-bbbbbb", "AB12CD")
	if err != nil || got != "ADM-BBBBBB" {
		t.Fatalf("a typed Agency ID should win, got %q (%v)", got, err)
	}

	if _, err := resolveOnboardingAgency(ctx, repo, "ADM-NOPE00", ""); err == nil {
		t.Fatal("an unknown Agency ID must be rejected")
	}
}

func TestRecordPendingReferralOnlyCreditsStaff(t *testing.T) {
	admin := staffAdmin("ADM-AAAAAA", "AB12CD")
	client := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleClient, Email: "c@x.in", ReferralCode: "LEGACY"}
	userRepo := newFakeUserRepo(admin, client)
	ctx := context.Background()

	refs := &fakeReferralRepo{}
	recordPendingReferral(ctx, refs, userRepo, "LEGACY", "new@x.in", "New Person", "9876543210")
	if len(refs.records) != 0 {
		t.Fatal("a client's referral code must not create a referral record")
	}

	recordPendingReferral(ctx, refs, userRepo, "AB12CD", "New@X.in", "New Person", "9876543210")
	if len(refs.records) != 1 {
		t.Fatalf("an admin's code should record a referral, got %d records", len(refs.records))
	}
	rec := refs.records[0]
	if rec.ReferrerID != admin.ID || rec.ReferredEmail != "new@x.in" || rec.ReferredName != "New Person" {
		t.Fatalf("unexpected record: %+v", rec)
	}

	// Re-applying (e.g. a resubmitted request) must not double-count.
	recordPendingReferral(ctx, refs, userRepo, "AB12CD", "new@x.in", "New Person", "")
	if len(refs.records) != 1 {
		t.Fatalf("a second application must not add another pending referral, got %d", len(refs.records))
	}

	// Nobody refers themselves.
	recordPendingReferral(ctx, refs, userRepo, "AB12CD", admin.Email, admin.Name, "")
	if len(refs.records) != 1 {
		t.Fatal("a self-referral must be ignored")
	}
}

// Approving a referred applicant credits the admin and stamps the account that was created.
func TestApproveRequestCompletesReferral(t *testing.T) {
	admin := staffAdmin("ADM-AAAAAA", "AB12CD")
	userRepo := newFakeUserRepo(admin)
	refs := &fakeReferralRepo{}
	ctx := context.Background()

	req := &domain.AccessRequest{
		Name: "Riya Das", Email: "riya@x.in", Phone: "9876500000",
		AppliedReferralCode: "AB12CD", AppliedAgencyID: admin.AdminID,
	}
	reqRepo := newFakeAccessReqRepo(req)
	recordPendingReferral(ctx, refs, userRepo, "AB12CD", req.Email, req.Name, req.Phone)

	svc := NewAccessRequestService(reqRepo, userRepo, newTestUserService(userRepo), nil, refs)

	created, err := svc.ApproveRequest(ctx, domain.RoleAdmin, admin.ID.Hex(), req.ID.Hex(), nil)
	if err != nil {
		t.Fatalf("approval failed: %v", err)
	}
	if created.ReferralCode != "" {
		t.Fatal("an approved client must not be issued a referral code")
	}
	if created.AgencyID != admin.AdminID {
		t.Fatalf("client should be filed under the referring admin, got %q", created.AgencyID)
	}

	rec := refs.records[0]
	if rec.Status != domain.ReferralStatusCompleted {
		t.Fatalf("referral should be completed, got %q", rec.Status)
	}
	if rec.ReferredUserID == nil || *rec.ReferredUserID != created.ID {
		t.Fatal("completed referral should point at the account it produced")
	}
	if rec.CompletedAt == nil {
		t.Fatal("completed referral should be timestamped")
	}
}

func TestReferralStatsAreStaffOnly(t *testing.T) {
	admin := staffAdmin("ADM-AAAAAA", "AB12CD")
	client := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleClient, Email: "c@x.in", IsActive: true}
	userRepo := newFakeUserRepo(admin, client)
	refs := &fakeReferralRepo{}
	svc := NewReferralService(refs, userRepo)
	ctx := context.Background()

	if _, err := svc.GetMyStats(ctx, domain.RoleClient, client.ID.Hex()); err == nil {
		t.Fatal("a client has no referral code and must be refused")
	}

	_, _ = refs.Create(ctx, &domain.ReferralRecord{ReferrerID: admin.ID, ReferredEmail: "a@x.in"})
	done, _ := refs.Create(ctx, &domain.ReferralRecord{ReferrerID: admin.ID, ReferredEmail: "b@x.in"})
	_ = refs.Complete(ctx, done.ID, bson.NewObjectID(), "B", "")

	stats, err := svc.GetMyStats(ctx, domain.RoleAdmin, admin.ID.Hex())
	if err != nil {
		t.Fatalf("admin stats failed: %v", err)
	}
	if stats.ReferralCode != "AB12CD" || stats.TotalPending != 1 || stats.TotalCompleted != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestReferralLedgerScoping(t *testing.T) {
	admin := staffAdmin("ADM-AAAAAA", "AB12CD")
	other := staffAdmin("ADM-BBBBBB", "ZZ99ZZ")
	superAdmin := &domain.User{ID: bson.NewObjectID(), Role: domain.RoleSuperAdmin, Email: "s@x.in", IsActive: true, AdminID: "ADM-SUPER0"}
	userRepo := newFakeUserRepo(admin, other, superAdmin)
	refs := &fakeReferralRepo{}
	ctx := context.Background()
	_, _ = refs.Create(ctx, &domain.ReferralRecord{ReferrerID: admin.ID, ReferredEmail: "mine@x.in"})
	_, _ = refs.Create(ctx, &domain.ReferralRecord{ReferrerID: other.ID, ReferredEmail: "theirs@x.in"})

	svc := NewReferralService(refs, userRepo)

	mine, err := svc.GetAllReferrals(ctx, domain.RoleAdmin, admin.ID.Hex(), "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine.Data) != 1 || mine.Data[0].ReferredEmail != "mine@x.in" {
		t.Fatalf("a plain admin must only see their own referrals, got %+v", mine.Data)
	}
	// A plain admin can't peek at another admin's list by passing their ID.
	spied, err := svc.GetAllReferrals(ctx, domain.RoleAdmin, admin.ID.Hex(), other.ID.Hex(), 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(spied.Data) != 1 || spied.Data[0].ReferredEmail != "mine@x.in" {
		t.Fatalf("referrer_id must be ignored for a plain admin, got %+v", spied.Data)
	}

	all, err := svc.GetAllReferrals(ctx, domain.RoleSuperAdmin, superAdmin.ID.Hex(), "", 1, 20)
	if err != nil || len(all.Data) != 2 {
		t.Fatalf("a super admin should see every referral, got %d (%v)", len(all.Data), err)
	}
	filtered, err := svc.GetAllReferrals(ctx, domain.RoleSuperAdmin, superAdmin.ID.Hex(), other.ID.Hex(), 1, 20)
	if err != nil || len(filtered.Data) != 1 || filtered.Data[0].ReferredEmail != "theirs@x.in" {
		t.Fatalf("a super admin should be able to drill into one admin, got %+v (%v)", filtered.Data, err)
	}
	if _, err := svc.GetAllReferrals(ctx, domain.RoleClient, bson.NewObjectID().Hex(), "", 1, 20); err == nil {
		t.Fatal("a client must not read the ledger")
	}
}

func TestAdminReferralSummary(t *testing.T) {
	quiet := staffAdmin("ADM-QUIET0", "QQ11QQ")
	busy := staffAdmin("ADM-BUSY00", "BB22BB")
	userRepo := newFakeUserRepo(quiet, busy)
	refs := &fakeReferralRepo{}
	ctx := context.Background()

	for i, email := range []string{"a@x.in", "b@x.in", "c@x.in"} {
		rec, _ := refs.Create(ctx, &domain.ReferralRecord{ReferrerID: busy.ID, ReferredEmail: email})
		if i < 2 {
			_ = refs.Complete(ctx, rec.ID, bson.NewObjectID(), "Client", "")
		}
	}

	svc := NewReferralService(refs, userRepo)

	if _, err := svc.GetAdminSummary(ctx, domain.RoleAdmin); err == nil {
		t.Fatal("only a super admin may read the leaderboard")
	}

	summary, err := svc.GetAdminSummary(ctx, domain.RoleSuperAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary) != 2 {
		t.Fatalf("every staff account belongs in the summary, got %d", len(summary))
	}
	if summary[0].AdminUserID != busy.ID || summary[0].TotalCompleted != 2 || summary[0].TotalPending != 1 || summary[0].Total != 3 {
		t.Fatalf("busiest admin should lead with its counts: %+v", summary[0])
	}
	if summary[1].AdminUserID != quiet.ID || summary[1].Total != 0 {
		t.Fatalf("an admin with no referrals should still be listed with zeros: %+v", summary[1])
	}
	if summary[0].ReferralCode != "BB22BB" || summary[0].AdminID != "ADM-BUSY00" {
		t.Fatalf("summary should carry the admin's code and ID: %+v", summary[0])
	}
}

// A self-service signup gets no referral code, no validity date, and lands in the agency whose
// referral code it used.
func TestRegisterUsesReferralCodeForAgencyAndIssuesNoCode(t *testing.T) {
	admin := staffAdmin("ADM-AAAAAA", "AB12CD")
	repo := newFakeUserRepo(admin)
	refs := &fakeReferralRepo{}
	svc := newTestUserService(repo)
	if setter, ok := svc.(interface {
		SetReferralRepository(domain.ReferralRepository)
	}); ok {
		setter.SetReferralRepository(refs)
	}

	created, err := svc.Register(context.Background(), &domain.CreateUserRequest{
		Name: "Riya", Email: "riya@x.in", Password: "secret1", Phone: "9876500000", ReferralCode: "ab12cd",
	})
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}
	if created.ReferralCode != "" {
		t.Fatal("a client must not be issued a referral code")
	}
	if created.AgencyID != "ADM-AAAAAA" {
		t.Fatalf("the referral code should file the signup under that admin, got %q", created.AgencyID)
	}
	if len(refs.records) != 1 || refs.records[0].ReferrerID != admin.ID {
		t.Fatalf("signup should record a pending referral for the admin, got %+v", refs.records)
	}
}

// A deactivated or retired admin's code must stop onboarding clients: nobody could sign in to
// manage them, and the agency link would point at a dead account.
func TestInactiveStaffCodeIsIgnored(t *testing.T) {
	retired := staffAdmin("ADM-GONE00", "GG00GG")
	retired.IsActive = false
	mergedInto := bson.NewObjectID()
	merged := staffAdmin("ADM-MERGE0", "MM00MM")
	merged.MergedIntoUserID = &mergedInto
	repo := newFakeUserRepo(retired, merged)
	refs := &fakeReferralRepo{}
	ctx := context.Background()

	for _, code := range []string{"GG00GG", "MM00MM"} {
		agency, err := resolveOnboardingAgency(ctx, repo, "", code)
		if err != nil || agency != "" {
			t.Fatalf("code %s should not set an agency, got %q (%v)", code, agency, err)
		}
		recordPendingReferral(ctx, refs, repo, code, "new@x.in", "New", "")
	}
	if len(refs.records) != 0 {
		t.Fatalf("no referral should be recorded for inactive staff, got %d", len(refs.records))
	}
}
