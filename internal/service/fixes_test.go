package service

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/smart-invest-solutions/backend/internal/config"
	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"
)

// fakeUserRepo is an in-memory UserRepository. Embedding the interface means any method a test
// doesn't exercise panics loudly instead of needing a stub.
type fakeUserRepo struct {
	domain.UserRepository
	users map[bson.ObjectID]*domain.User
}

func newFakeUserRepo(users ...*domain.User) *fakeUserRepo {
	r := &fakeUserRepo{users: map[bson.ObjectID]*domain.User{}}
	for _, u := range users {
		if u.ID.IsZero() {
			u.ID = bson.NewObjectID()
		}
		r.users[u.ID] = u
	}
	return r
}

func (r *fakeUserRepo) FindByID(_ context.Context, id bson.ObjectID) (*domain.User, error) {
	if u, ok := r.users[id]; ok {
		return u, nil
	}
	return nil, domain.ErrUserNotFound
}

func (r *fakeUserRepo) FindByEmail(_ context.Context, email string) (*domain.User, error) {
	for _, u := range r.users {
		if strings.EqualFold(u.Email, strings.TrimSpace(email)) {
			return u, nil
		}
	}
	return nil, domain.ErrUserNotFound
}

func (r *fakeUserRepo) FindByAdminID(_ context.Context, adminID string) (*domain.User, error) {
	for _, u := range r.users {
		if u.AdminID != "" && u.AdminID == adminID {
			return u, nil
		}
	}
	return nil, domain.ErrUserNotFound
}

func (r *fakeUserRepo) FindByReferralCode(_ context.Context, code string) (*domain.User, error) {
	for _, u := range r.users {
		if u.ReferralCode == code {
			return u, nil
		}
	}
	return nil, domain.ErrUserNotFound
}

func (r *fakeUserRepo) UpdatePassword(_ context.Context, id bson.ObjectID, hashed string) error {
	r.users[id].Password = hashed
	return nil
}

func (r *fakeUserRepo) RecordFailedLogin(_ context.Context, id bson.ObjectID) (int, error) {
	r.users[id].FailedLoginAttempts++
	return r.users[id].FailedLoginAttempts, nil
}

func (r *fakeUserRepo) LockAccount(context.Context, bson.ObjectID, time.Time) error { return nil }

func (r *fakeUserRepo) ClearFailedLogins(_ context.Context, id bson.ObjectID) error {
	r.users[id].FailedLoginAttempts = 0
	return nil
}

func (r *fakeUserRepo) Delete(_ context.Context, id bson.ObjectID) error {
	delete(r.users, id)
	return nil
}

func (r *fakeUserRepo) Update(_ context.Context, id bson.ObjectID, req *domain.UpdateUserRequest) (*domain.User, error) {
	u := r.users[id]
	if req.Role != nil {
		u.Role = *req.Role
	}
	if req.IsActive != nil {
		u.IsActive = *req.IsActive
	}
	if req.AgencyID != nil {
		u.AgencyID = *req.AgencyID
	}
	return u, nil
}

func hash(t *testing.T, secret string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}

func newTestUserService(repo domain.UserRepository) domain.UserService {
	return NewUserService(repo, &config.Config{JWTSecret: "test-secret", JWTExpiryHours: "1"}, nil)
}

// An anonymous signup must never be able to rewrite an admin account's password — admins are
// created without is_email_verified, which the old "restart unverified signup" path treated as an
// abandoned registration.
func TestRegisterCannotOverwriteAdminAccount(t *testing.T) {
	original := hash(t, "admin-secret")
	admin := &domain.User{Email: "boss@agency.in", Role: domain.RoleAdmin, IsActive: true, Password: original, AdminID: "ADM-AAAAAA"}
	svc := newTestUserService(newFakeUserRepo(admin))

	_, err := svc.Register(context.Background(), &domain.CreateUserRequest{
		Name: "Attacker", Email: "BOSS@agency.in", Password: "hijacked",
	})
	if err == nil {
		t.Fatal("expected registration against an admin email to be refused")
	}
	if admin.Password != original {
		t.Fatal("admin password was overwritten by an anonymous signup")
	}
}

func TestRegisterRejectsUnknownAgency(t *testing.T) {
	svc := newTestUserService(newFakeUserRepo())
	_, err := svc.Register(context.Background(), &domain.CreateUserRequest{
		Name: "New", Email: "new@x.in", Password: "secret1", AgencyID: "ADM-NOPE00",
	})
	if err == nil || !strings.Contains(err.Error(), "Agency ID") {
		t.Fatalf("expected an invalid Agency ID error, got %v", err)
	}
}

// A wrong password must look identical whether the account is active, pending or deactivated.
func TestLoginWrongPasswordDoesNotRevealAccountState(t *testing.T) {
	pending := &domain.User{Email: "pending@x.in", Role: domain.RoleClient, IsEmailVerified: true, IsActive: false, Password: hash(t, "right-pass")}
	svc := newTestUserService(newFakeUserRepo(pending))

	_, err := svc.Login(context.Background(), &domain.UserLoginRequest{Identifier: "pending@x.in", Password: "wrong-pass"})
	if err == nil || err.Error() != "invalid credentials" {
		t.Fatalf("expected a generic invalid-credentials error, got %v", err)
	}

	_, err = svc.Login(context.Background(), &domain.UserLoginRequest{Identifier: "pending@x.in", Password: "right-pass"})
	if err == nil || !strings.Contains(err.Error(), "pending verification") {
		t.Fatalf("with the right password the pending state should be explained, got %v", err)
	}
}

func TestUpdateRefusesStaffRoleAndCrossAgencyEdits(t *testing.T) {
	admin := &domain.User{Role: domain.RoleAdmin, AdminID: "ADM-OWN001", IsActive: true}
	other := &domain.User{Role: domain.RoleAdmin, AdminID: "ADM-OTH001", IsActive: true}
	mine := &domain.User{Role: domain.RoleClient, AgencyID: "ADM-OWN001", IsActive: true}
	theirs := &domain.User{Role: domain.RoleClient, AgencyID: "ADM-OTH001", IsActive: true}
	svc := newTestUserService(newFakeUserRepo(admin, other, mine, theirs))
	ctx := context.Background()

	promote := domain.RoleAdmin
	if _, err := svc.Update(ctx, domain.RoleAdmin, admin.ID.Hex(), mine.ID.Hex(), &domain.UpdateUserRequest{Role: &promote}); err == nil {
		t.Fatal("a plain admin must not promote a client to admin")
	}
	if _, err := svc.Update(ctx, domain.RoleSuperAdmin, bson.NewObjectID().Hex(), mine.ID.Hex(), &domain.UpdateUserRequest{Role: &promote}); err == nil {
		t.Fatal("even a super admin must use CreateAdmin rather than promoting through Update")
	}

	inactive := false
	if _, err := svc.Update(ctx, domain.RoleAdmin, admin.ID.Hex(), theirs.ID.Hex(), &domain.UpdateUserRequest{IsActive: &inactive}); err == nil {
		t.Fatal("a plain admin must not edit another agency's client")
	}
	moveTo := "ADM-OTH001"
	if _, err := svc.Update(ctx, domain.RoleAdmin, admin.ID.Hex(), mine.ID.Hex(), &domain.UpdateUserRequest{AgencyID: &moveTo}); err == nil {
		t.Fatal("only a super admin may move a client between agencies")
	}
	advisor := domain.RoleAdvisor
	if _, err := svc.Update(ctx, domain.RoleAdmin, admin.ID.Hex(), mine.ID.Hex(), &domain.UpdateUserRequest{Role: &advisor}); err != nil {
		t.Fatalf("switching an own-agency client to advisor should work: %v", err)
	}
}

func TestDeleteIsAgencyScoped(t *testing.T) {
	admin := &domain.User{Role: domain.RoleAdmin, AdminID: "ADM-OWN001", IsActive: true}
	theirs := &domain.User{Role: domain.RoleClient, AgencyID: "ADM-OTH001", IsActive: true, Email: "c@x.in"}
	mine := &domain.User{Role: domain.RoleClient, AgencyID: "ADM-OWN001", IsActive: true, Email: "m@x.in"}
	repo := newFakeUserRepo(admin, theirs, mine)
	svc := newTestUserService(repo)
	ctx := context.Background()

	if err := svc.Delete(ctx, domain.RoleAdmin, admin.ID.Hex(), theirs.ID.Hex()); err == nil {
		t.Fatal("a plain admin must not delete another agency's client")
	}
	if _, ok := repo.users[theirs.ID]; !ok {
		t.Fatal("cross-agency client was deleted")
	}
	if err := svc.Delete(ctx, domain.RoleAdmin, admin.ID.Hex(), mine.ID.Hex()); err != nil {
		t.Fatalf("own-agency delete should succeed: %v", err)
	}
	if err := svc.Delete(ctx, domain.RoleAdmin, admin.ID.Hex(), admin.ID.Hex()); err == nil {
		t.Fatal("self-delete through the staff endpoint must be refused")
	}
}

func TestBuildUpcomingPaymentsKeepsTodayAndOverdue(t *testing.T) {
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC) // 15:30 IST
	dueTodayMidnightUTC := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	overdue := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	stale := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	farFuture := time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC)
	doc := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	maturity := time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC)

	life := func(plan string, due time.Time) *domain.LifeInsurance {
		return &domain.LifeInsurance{
			PolicyDetails:  domain.PolicyDetails{PlanName: plan, DOC: doc, PPT: 20, MaturityDate: maturity},
			PremiumDetails: domain.PremiumDetails{NextDueDate: due, InstallmentPremium: 1000},
		}
	}
	finished := life("paid-up", overdue)
	finished.PolicyDetails.PPT = 5 // premiums ended in 2025

	got := buildUpcomingPayments(
		[]*domain.LifeInsurance{life("today", dueTodayMidnightUTC), life("late", overdue), life("stale", stale), life("later", farFuture), finished},
		nil,
		[]*domain.GeneralInsurance{{VehicleNo: "WB01", DateOfExpiry: "2026-09-10"}},
		[]*domain.FixedDeposit{{FDName: "matured", MaturityDate: overdue}},
		now,
	)

	names := []string{}
	for _, item := range got {
		names = append(names, item.EntityName)
	}
	if len(got) != 3 {
		t.Fatalf("expected [late, WB01, today], got %v", names)
	}
	if !got[0].IsOverdue || !got[1].IsOverdue || got[2].IsOverdue {
		t.Fatalf("overdue items must sort first and be flagged: %+v", got)
	}
	if got[2].EntityName != "today" {
		t.Fatalf("a premium due today must still be listed, got %v", names)
	}
}

func TestNextPremiumDueDateClampsToAnchorDay(t *testing.T) {
	anchor := time.Date(2020, 1, 31, 0, 0, 0, 0, time.UTC)
	jan := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)

	feb, err := nextPremiumDueDate(jan, domain.PaymentModeMonthly, anchor)
	if err != nil || !feb.Equal(time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("Jan 31 + 1 month should be Feb 28, got %v (%v)", feb, err)
	}
	mar, _ := nextPremiumDueDate(feb, domain.PaymentModeMonthly, anchor)
	if !mar.Equal(time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("the schedule should return to the 31st, got %v", mar)
	}
	yearly, _ := nextPremiumDueDate(jan, domain.PaymentModeYearly, anchor)
	if !yearly.Equal(time.Date(2027, 1, 31, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("yearly advance wrong: %v", yearly)
	}
	if _, err := nextPremiumDueDate(jan, "SSS", anchor); err == nil {
		t.Fatal("an unknown payment mode must be refused")
	}
}

func TestNormalizeISODate(t *testing.T) {
	if got, err := normalizeISODate("2026-02-03", "x"); err != nil || got != "2026-02-03" {
		t.Fatalf("got %q %v", got, err)
	}
	if got, err := normalizeISODate("2026-02-03T12:00:00Z", "x"); err != nil || got != "2026-02-03" {
		t.Fatalf("RFC3339 should reduce to a date, got %q %v", got, err)
	}
	if _, err := normalizeISODate("03/02/2026", "x"); err == nil {
		t.Fatal("a non-ISO date must be refused")
	}
}

func TestFormatAmountIndianGrouping(t *testing.T) {
	cases := map[float64]string{0: "Rs. 0", 999: "Rs. 999", 1000: "Rs. 1,000", 1234567: "Rs. 12,34,567", 123456789.4: "Rs. 12,34,56,789"}
	for in, want := range cases {
		if got := formatAmount(in); got != want {
			t.Errorf("formatAmount(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderPortfolioPDFHandlesLongAndAccentedText(t *testing.T) {
	member := &domain.FamilyMember{ID: bson.NewObjectID(), Name: "José Müller", RelationWithHOF: "Self", DateOfBirth: "1980-05-06"}
	out, err := renderPortfolioPDF(
		&domain.User{Name: "José Müller", Email: "j@x.in"},
		[]*domain.FamilyMember{member},
		[]*domain.LifeInsurance{{FamilyMemberID: member.ID, PolicyDetails: domain.PolicyDetails{PolicyNo: "123456789", PlanName: strings.Repeat("Very Long Plan Name ", 10)}}},
		nil, nil,
		[]*domain.FixedDeposit{{FamilyMemberID: member.ID, FDName: "FD", CompanyName: "Bank", PrincipalAmount: 100000, MaturityAmount: 120000}},
	)
	if err != nil || !bytes.HasPrefix(out, []byte("%PDF")) {
		t.Fatalf("expected a PDF, got err=%v", err)
	}
}

func TestCompressImageFlattensTransparencyOntoWhite(t *testing.T) {
	// Photo-like noise keeps the PNG well above the compression threshold; the corner stays transparent.
	rng := rand.New(rand.NewSource(1))
	src := image.NewNRGBA(image.Rect(0, 0, 2500, 1500))
	for y := 0; y < 1500; y++ {
		for x := 0; x < 2500; x++ {
			if x < 100 && y < 100 {
				continue
			}
			src.Set(x, y, color.NRGBA{uint8(rng.Intn(256)), uint8(rng.Intn(256)), uint8(rng.Intn(256)), 255})
		}
	}
	var in bytes.Buffer
	if err := png.Encode(&in, src); err != nil {
		t.Fatal(err)
	}

	out, ok := compressImageBuffer(in.Bytes())
	if !ok {
		t.Fatal("a large PNG should be re-encoded")
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() > 1920 || img.Bounds().Dy() > 1920 {
		t.Fatalf("image not resized: %v", img.Bounds())
	}
	r, g, b, _ := img.At(5, 5).RGBA()
	if r < 0xE000 || g < 0xE000 || b < 0xE000 {
		t.Fatalf("transparent area should be white, got %v %v %v", r>>8, g>>8, b>>8)
	}
}

func TestCompressImageKeepsOriginalWhenNotSmaller(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 50, 50))
	var in bytes.Buffer
	if err := png.Encode(&in, src); err != nil {
		t.Fatal(err)
	}
	out, ok := compressImageBuffer(in.Bytes())
	if ok || !bytes.Equal(out, in.Bytes()) {
		t.Fatal("a re-encode that isn't smaller must keep the original bytes")
	}
}

func TestExtractTextFromPDFRejectsGarbageWithoutPanicking(t *testing.T) {
	for _, input := range [][]byte{
		[]byte("not a pdf at all"),
		[]byte("%PDF-1.7\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF"),
	} {
		if _, err := extractTextFromPDF(input); err == nil {
			t.Errorf("expected an error for malformed input %q", input[:10])
		}
	}
}
