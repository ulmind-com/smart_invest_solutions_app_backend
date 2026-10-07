package service

import (
	"context"
	"strings"
	"testing"

	"github.com/smart-invest-solutions/backend/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

/* ---------------------------------------------------------------- fakes */

// fakeProductRepo is an in-memory catalog. Embedding the interface means a method a test doesn't
// exercise panics loudly instead of needing a stub.
type fakeProductRepo struct {
	domain.ProductRepository
	products  map[bson.ObjectID]*domain.Product
	lastQuery *domain.ProductQuery
	deleted   []bson.ObjectID
}

func newFakeProductRepo(products ...*domain.Product) *fakeProductRepo {
	r := &fakeProductRepo{products: map[bson.ObjectID]*domain.Product{}}
	for _, p := range products {
		if p.ID.IsZero() {
			p.ID = bson.NewObjectID()
		}
		r.products[p.ID] = p
	}
	return r
}

func (r *fakeProductRepo) FindByID(_ context.Context, id bson.ObjectID) (*domain.Product, error) {
	if p, ok := r.products[id]; ok {
		return p, nil
	}
	return nil, errProductNotFound
}

func (r *fakeProductRepo) FindExistingIDs(_ context.Context, ids []bson.ObjectID) ([]bson.ObjectID, error) {
	found := make([]bson.ObjectID, 0, len(ids))
	for _, id := range ids {
		if _, ok := r.products[id]; ok {
			found = append(found, id)
		}
	}
	return found, nil
}

func (r *fakeProductRepo) FindAll(_ context.Context, query domain.ProductQuery) ([]*domain.Product, int64, error) {
	q := query
	r.lastQuery = &q

	matched := make([]*domain.Product, 0, len(r.products))
	for _, p := range r.products {
		if query.IsActive != nil && p.IsActive != *query.IsActive {
			continue
		}
		if query.Restricted {
			allowed := false
			for _, id := range query.AllowedIDs {
				if id == p.ID {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}
		matched = append(matched, p)
	}
	return matched, int64(len(matched)), nil
}

func (r *fakeProductRepo) Delete(_ context.Context, id bson.ObjectID) error {
	delete(r.products, id)
	r.deleted = append(r.deleted, id)
	return nil
}

var errProductNotFound = &notFoundError{}

type notFoundError struct{}

func (e *notFoundError) Error() string { return "product not found" }

// fakeAccessRepo is an in-memory store of per-client catalog overrides.
type fakeAccessRepo struct {
	domain.ClientProductAccessRepository
	records map[bson.ObjectID]*domain.ClientProductAccess
	pruned  []bson.ObjectID
}

func newFakeAccessRepo() *fakeAccessRepo {
	return &fakeAccessRepo{records: map[bson.ObjectID]*domain.ClientProductAccess{}}
}

func (r *fakeAccessRepo) FindByUserID(_ context.Context, userID bson.ObjectID) (*domain.ClientProductAccess, error) {
	return r.records[userID], nil
}

func (r *fakeAccessRepo) Upsert(_ context.Context, access *domain.ClientProductAccess) (*domain.ClientProductAccess, error) {
	saved := *access
	if saved.ProductIDs == nil {
		saved.ProductIDs = []bson.ObjectID{}
	}
	r.records[access.UserID] = &saved
	return &saved, nil
}

func (r *fakeAccessRepo) DeleteByUserID(_ context.Context, userID bson.ObjectID) error {
	delete(r.records, userID)
	return nil
}

func (r *fakeAccessRepo) RemoveProductFromAll(_ context.Context, productID bson.ObjectID) error {
	r.pruned = append(r.pruned, productID)
	for _, record := range r.records {
		kept := make([]bson.ObjectID, 0, len(record.ProductIDs))
		for _, id := range record.ProductIDs {
			if id != productID {
				kept = append(kept, id)
			}
		}
		record.ProductIDs = kept
	}
	return nil
}

/* ------------------------------------------------------------- fixtures */

type accessFixture struct {
	admin      *domain.User
	otherAdmin *domain.User
	superAdmin *domain.User
	client     *domain.User
	foreign    *domain.User
	life       *domain.Product
	h1         *domain.Product
	h2         *domain.Product
	h3         *domain.Product
	draft      *domain.Product
	products   *fakeProductRepo
	accessRepo *fakeAccessRepo
	users      *fakeUserRepo
	svc        domain.ClientProductAccessService
}

func newAccessFixture() *accessFixture {
	f := &accessFixture{
		admin:      &domain.User{ID: bson.NewObjectID(), Name: "Asha Nair", Role: domain.RoleAdmin, AdminID: "ADM-ASHA01"},
		otherAdmin: &domain.User{ID: bson.NewObjectID(), Name: "Bikash Roy", Role: domain.RoleAdmin, AdminID: "ADM-BIKASH"},
		superAdmin: &domain.User{ID: bson.NewObjectID(), Name: "Head Office", Role: domain.RoleSuperAdmin},
		client:     &domain.User{ID: bson.NewObjectID(), Name: "Riya Sen", Role: domain.RoleClient, AgencyID: "ADM-ASHA01"},
		foreign:    &domain.User{ID: bson.NewObjectID(), Name: "Sanjay Das", Role: domain.RoleClient, AgencyID: "ADM-BIKASH"},
		life:       &domain.Product{ID: bson.NewObjectID(), Name: "Jeevan Anand", Category: domain.ProductCategoryLife, IsActive: true},
		h1:         &domain.Product{ID: bson.NewObjectID(), Name: "Health One", Category: domain.ProductCategoryHealth, IsActive: true},
		h2:         &domain.Product{ID: bson.NewObjectID(), Name: "Health Two", Category: domain.ProductCategoryHealth, IsActive: true},
		h3:         &domain.Product{ID: bson.NewObjectID(), Name: "Health Three", Category: domain.ProductCategoryHealth, IsActive: true},
		draft:      &domain.Product{ID: bson.NewObjectID(), Name: "Unpublished", Category: domain.ProductCategoryFD, IsActive: false},
	}
	f.products = newFakeProductRepo(f.life, f.h1, f.h2, f.h3, f.draft)
	f.accessRepo = newFakeAccessRepo()
	f.users = newFakeUserRepo(f.admin, f.otherAdmin, f.superAdmin, f.client, f.foreign)
	f.svc = NewClientProductAccessService(f.accessRepo, f.users, f.products)
	return f
}

/* ---------------------------------------------------------------- tests */

func TestAClientWithNoOverrideSeesTheWholeCatalog(t *testing.T) {
	// The default has to be "everything": adding this feature must not blank anybody's Products tab.
	f := newAccessFixture()

	dto, err := f.svc.GetForClient(context.Background(), domain.RoleAdmin, f.admin.ID.Hex(), f.client.ID.Hex())
	if err != nil {
		t.Fatalf("GetForClient: %v", err)
	}
	if dto.Mode != domain.ProductAccessAll {
		t.Errorf("mode = %q, want %q", dto.Mode, domain.ProductAccessAll)
	}
	if dto.Configured {
		t.Error("a client nobody restricted must not read as configured")
	}
	if dto.ProductIDs == nil {
		t.Error("product IDs must be an empty slice, not nil")
	}

	visibility, err := f.svc.ResolveVisibility(context.Background(), domain.RoleClient, f.client.ID.Hex())
	if err != nil {
		t.Fatalf("ResolveVisibility: %v", err)
	}
	if visibility.Restricted {
		t.Error("an unconfigured client must not be restricted")
	}
}

func TestAdminPicksExactlyWhichProductsOneClientSees(t *testing.T) {
	// The business case: of three health products, this client gets only the second, plus one other.
	f := newAccessFixture()

	dto, err := f.svc.SetForClient(context.Background(), domain.RoleAdmin, f.admin.ID.Hex(), f.client.ID.Hex(),
		&domain.SetClientProductAccessDTO{
			Mode:       domain.ProductAccessSelected,
			ProductIDs: []string{f.h2.ID.Hex(), f.life.ID.Hex()},
		})
	if err != nil {
		t.Fatalf("SetForClient: %v", err)
	}
	if dto.Mode != domain.ProductAccessSelected || dto.SelectedCount != 2 {
		t.Fatalf("mode = %q count = %d, want selected/2", dto.Mode, dto.SelectedCount)
	}
	if !dto.Configured || dto.UpdatedByName != "Asha Nair" {
		t.Errorf("the record should name who set it, got configured=%t by %q", dto.Configured, dto.UpdatedByName)
	}

	visibility, err := f.svc.ResolveVisibility(context.Background(), domain.RoleClient, f.client.ID.Hex())
	if err != nil {
		t.Fatalf("ResolveVisibility: %v", err)
	}
	if !visibility.Restricted {
		t.Fatal("the client must now be restricted")
	}
	if !visibility.Allows(f.h2.ID) || !visibility.Allows(f.life.ID) {
		t.Error("the two chosen products must be visible")
	}
	if visibility.Allows(f.h1.ID) || visibility.Allows(f.h3.ID) {
		t.Error("the health products that were not chosen must be hidden")
	}
}

func TestSwitchingBackToTheWholeCatalogForgetsTheSelection(t *testing.T) {
	f := newAccessFixture()
	ctx := context.Background()

	if _, err := f.svc.SetForClient(ctx, domain.RoleAdmin, f.admin.ID.Hex(), f.client.ID.Hex(),
		&domain.SetClientProductAccessDTO{Mode: domain.ProductAccessSelected, ProductIDs: []string{f.h2.ID.Hex()}}); err != nil {
		t.Fatalf("SetForClient(selected): %v", err)
	}

	// No product IDs are sent when restoring the full catalog, so the previous list must not survive
	// and quietly come back the next time the mode is switched.
	dto, err := f.svc.SetForClient(ctx, domain.RoleAdmin, f.admin.ID.Hex(), f.client.ID.Hex(),
		&domain.SetClientProductAccessDTO{Mode: domain.ProductAccessAll})
	if err != nil {
		t.Fatalf("SetForClient(all): %v", err)
	}
	if dto.Mode != domain.ProductAccessAll || dto.SelectedCount != 0 || len(dto.ProductIDs) != 0 {
		t.Errorf("expected an empty selection in mode all, got %q with %d", dto.Mode, dto.SelectedCount)
	}

	visibility, err := f.svc.ResolveVisibility(ctx, domain.RoleClient, f.client.ID.Hex())
	if err != nil {
		t.Fatalf("ResolveVisibility: %v", err)
	}
	if visibility.Restricted {
		t.Error("back on the whole catalog, the client must not be restricted")
	}
}

func TestSelectingNothingHidesEveryProduct(t *testing.T) {
	// Distinct from "never configured": an admin may deliberately show a client nothing.
	f := newAccessFixture()

	dto, err := f.svc.SetForClient(context.Background(), domain.RoleAdmin, f.admin.ID.Hex(), f.client.ID.Hex(),
		&domain.SetClientProductAccessDTO{Mode: domain.ProductAccessSelected, ProductIDs: []string{}})
	if err != nil {
		t.Fatalf("SetForClient: %v", err)
	}
	if !dto.Configured || dto.SelectedCount != 0 {
		t.Fatalf("expected a configured record with nothing selected, got configured=%t count=%d", dto.Configured, dto.SelectedCount)
	}

	visibility, err := f.svc.ResolveVisibility(context.Background(), domain.RoleClient, f.client.ID.Hex())
	if err != nil {
		t.Fatalf("ResolveVisibility: %v", err)
	}
	if !visibility.Restricted || len(visibility.AllowedIDs) != 0 {
		t.Errorf("expected a restriction allowing nothing, got restricted=%t with %d allowed", visibility.Restricted, len(visibility.AllowedIDs))
	}
	if visibility.Allows(f.h2.ID) {
		t.Error("no product may be visible")
	}
}

func TestAnAdminCannotTouchAnotherAgencysClient(t *testing.T) {
	f := newAccessFixture()
	ctx := context.Background()

	// Reported as not found, not as forbidden: the endpoint must not confirm that this client exists.
	_, err := f.svc.GetForClient(ctx, domain.RoleAdmin, f.admin.ID.Hex(), f.foreign.ID.Hex())
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected a not-found error reading another agency's client, got %v", err)
	}

	_, err = f.svc.SetForClient(ctx, domain.RoleAdmin, f.admin.ID.Hex(), f.foreign.ID.Hex(),
		&domain.SetClientProductAccessDTO{Mode: domain.ProductAccessSelected, ProductIDs: []string{f.h2.ID.Hex()}})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected a not-found error writing another agency's client, got %v", err)
	}
	if len(f.accessRepo.records) != 0 {
		t.Error("nothing may be written for a client outside the caller's agency")
	}
}

func TestASuperAdminMaySetAnyAgencysClient(t *testing.T) {
	f := newAccessFixture()

	if _, err := f.svc.SetForClient(context.Background(), domain.RoleSuperAdmin, f.superAdmin.ID.Hex(), f.foreign.ID.Hex(),
		&domain.SetClientProductAccessDTO{Mode: domain.ProductAccessSelected, ProductIDs: []string{f.h3.ID.Hex()}}); err != nil {
		t.Fatalf("SetForClient: %v", err)
	}
	if f.accessRepo.records[f.foreign.ID] == nil {
		t.Error("the super admin's change should have been written")
	}
}

func TestAClientCannotSetAnybodysCatalog(t *testing.T) {
	f := newAccessFixture()

	_, err := f.svc.SetForClient(context.Background(), domain.RoleClient, f.client.ID.Hex(), f.client.ID.Hex(),
		&domain.SetClientProductAccessDTO{Mode: domain.ProductAccessAll})
	if err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("expected access denied for a client, got %v", err)
	}
	if len(f.accessRepo.records) != 0 {
		t.Error("a client must not be able to write a catalog override")
	}
}

func TestAnAdminAccountHasNoCatalogOfItsOwn(t *testing.T) {
	f := newAccessFixture()

	_, err := f.svc.GetForClient(context.Background(), domain.RoleSuperAdmin, f.superAdmin.ID.Hex(), f.otherAdmin.ID.Hex())
	if err == nil || !strings.Contains(err.Error(), "only a client account") {
		t.Fatalf("expected the admin target to be rejected, got %v", err)
	}
}

func TestAnUnknownProductIsRejectedRatherThanDropped(t *testing.T) {
	// Silently dropping it would be the worst outcome: the admin believes they granted a product and
	// the client never sees it, with nothing on screen to explain why.
	f := newAccessFixture()
	missing := bson.NewObjectID()

	_, err := f.svc.SetForClient(context.Background(), domain.RoleAdmin, f.admin.ID.Hex(), f.client.ID.Hex(),
		&domain.SetClientProductAccessDTO{
			Mode:       domain.ProductAccessSelected,
			ProductIDs: []string{f.h2.ID.Hex(), missing.Hex()},
		})
	if err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Fatalf("expected the unknown product to be rejected, got %v", err)
	}
	if len(f.accessRepo.records) != 0 {
		t.Error("a rejected selection must not be partially saved")
	}

	_, err = f.svc.SetForClient(context.Background(), domain.RoleAdmin, f.admin.ID.Hex(), f.client.ID.Hex(),
		&domain.SetClientProductAccessDTO{Mode: domain.ProductAccessSelected, ProductIDs: []string{"not-an-id"}})
	if err == nil || !strings.Contains(err.Error(), "invalid product ID") {
		t.Fatalf("expected a malformed ID to be rejected, got %v", err)
	}
}

func TestDuplicateSelectionsAreCollapsed(t *testing.T) {
	f := newAccessFixture()

	dto, err := f.svc.SetForClient(context.Background(), domain.RoleAdmin, f.admin.ID.Hex(), f.client.ID.Hex(),
		&domain.SetClientProductAccessDTO{
			Mode:       domain.ProductAccessSelected,
			ProductIDs: []string{f.h2.ID.Hex(), " " + f.h2.ID.Hex() + " ", "", f.life.ID.Hex()},
		})
	if err != nil {
		t.Fatalf("SetForClient: %v", err)
	}
	if dto.SelectedCount != 2 {
		t.Errorf("selected count = %d, want 2 after collapsing the repeat", dto.SelectedCount)
	}
}

func TestStaffAreNeverRestricted(t *testing.T) {
	f := newAccessFixture()

	for _, role := range []string{domain.RoleAdmin, domain.RoleSuperAdmin} {
		visibility, err := f.svc.ResolveVisibility(context.Background(), role, f.admin.ID.Hex())
		if err != nil {
			t.Fatalf("ResolveVisibility(%s): %v", role, err)
		}
		if visibility.Restricted {
			t.Errorf("%s must see the whole catalog", role)
		}
	}
}

func TestAnUnreadableCallerSeesNothing(t *testing.T) {
	// Fail closed: if the caller can't be identified, the answer is "nothing", never "everything".
	f := newAccessFixture()

	visibility, err := f.svc.ResolveVisibility(context.Background(), domain.RoleClient, "not-an-object-id")
	if err != nil {
		t.Fatalf("ResolveVisibility: %v", err)
	}
	if !visibility.Restricted || len(visibility.AllowedIDs) != 0 {
		t.Errorf("expected nothing visible, got restricted=%t with %d allowed", visibility.Restricted, len(visibility.AllowedIDs))
	}
}

func TestADeletedProductLeavesNoStaleSelection(t *testing.T) {
	f := newAccessFixture()
	ctx := context.Background()
	productSvc := NewProductService(f.products, nil, f.svc, f.accessRepo)

	if _, err := f.svc.SetForClient(ctx, domain.RoleAdmin, f.admin.ID.Hex(), f.client.ID.Hex(),
		&domain.SetClientProductAccessDTO{
			Mode:       domain.ProductAccessSelected,
			ProductIDs: []string{f.h2.ID.Hex(), f.life.ID.Hex()},
		}); err != nil {
		t.Fatalf("SetForClient: %v", err)
	}

	if err := productSvc.DeleteProduct(ctx, domain.RoleSuperAdmin, f.h2.ID.Hex()); err != nil {
		t.Fatalf("DeleteProduct: %v", err)
	}

	dto, err := f.svc.GetForClient(ctx, domain.RoleAdmin, f.admin.ID.Hex(), f.client.ID.Hex())
	if err != nil {
		t.Fatalf("GetForClient: %v", err)
	}
	if dto.SelectedCount != 1 || (len(dto.ProductIDs) == 1 && dto.ProductIDs[0] != f.life.ID.Hex()) {
		t.Errorf("expected only the surviving product, got %v", dto.ProductIDs)
	}

	visibility, err := f.svc.ResolveVisibility(ctx, domain.RoleClient, f.client.ID.Hex())
	if err != nil {
		t.Fatalf("ResolveVisibility: %v", err)
	}
	if visibility.Allows(f.h2.ID) {
		t.Error("a deleted product must not stay visible")
	}
}

/* --------------------------------- the catalog actually honouring it */

func TestTheCatalogOnlyReturnsWhatTheClientMaySee(t *testing.T) {
	f := newAccessFixture()
	ctx := context.Background()
	productSvc := NewProductService(f.products, nil, f.svc, f.accessRepo)

	if _, err := f.svc.SetForClient(ctx, domain.RoleAdmin, f.admin.ID.Hex(), f.client.ID.Hex(),
		&domain.SetClientProductAccessDTO{
			Mode:       domain.ProductAccessSelected,
			ProductIDs: []string{f.h2.ID.Hex()},
		}); err != nil {
		t.Fatalf("SetForClient: %v", err)
	}

	list, err := productSvc.GetAllProducts(ctx, domain.RoleClient, f.client.ID.Hex(), 1, 50, "", nil)
	if err != nil {
		t.Fatalf("GetAllProducts: %v", err)
	}
	if list.Total != 1 || len(list.Data) != 1 || list.Data[0].ID != f.h2.ID {
		t.Fatalf("expected only the one granted product, got total=%d data=%d", list.Total, len(list.Data))
	}
	// The filter has to reach the query: a page filtered afterwards would report the wrong total.
	if f.products.lastQuery == nil || !f.products.lastQuery.Restricted {
		t.Error("the restriction must be part of the database query, not applied to the page")
	}

	// A hidden product must not open by ID either — a link, a cached list or a hand-written request
	// would otherwise walk straight past the list filter.
	if _, err := productSvc.GetProductByID(ctx, domain.RoleClient, f.client.ID.Hex(), f.h1.ID.Hex()); err == nil {
		t.Error("a hidden product must not be readable by ID")
	}
	if _, err := productSvc.GetProductByID(ctx, domain.RoleClient, f.client.ID.Hex(), f.h2.ID.Hex()); err != nil {
		t.Errorf("the granted product must still open: %v", err)
	}

	// Another client of the same agency is untouched by this one's restriction.
	other, err := productSvc.GetAllProducts(ctx, domain.RoleClient, f.foreign.ID.Hex(), 1, 50, "", nil)
	if err != nil {
		t.Fatalf("GetAllProducts(other): %v", err)
	}
	if other.Total != 4 {
		t.Errorf("an unrestricted client should see all 4 published products, got %d", other.Total)
	}
}

func TestStaffStillBrowseTheFullCatalogIncludingDrafts(t *testing.T) {
	f := newAccessFixture()
	ctx := context.Background()
	productSvc := NewProductService(f.products, nil, f.svc, f.accessRepo)

	list, err := productSvc.GetAllProducts(ctx, domain.RoleAdmin, f.admin.ID.Hex(), 1, 50, "", nil)
	if err != nil {
		t.Fatalf("GetAllProducts: %v", err)
	}
	if list.Total != 5 {
		t.Errorf("staff should see all 5 products including the draft, got %d", list.Total)
	}
	if f.products.lastQuery.Restricted {
		t.Error("staff must never be restricted")
	}
}

func TestAGrantedButUnpublishedProductStaysHidden(t *testing.T) {
	// Visibility narrows the catalog; it never widens it. A draft granted by an admin must still
	// wait for the super admin to publish it.
	f := newAccessFixture()
	ctx := context.Background()
	productSvc := NewProductService(f.products, nil, f.svc, f.accessRepo)

	if _, err := f.svc.SetForClient(ctx, domain.RoleAdmin, f.admin.ID.Hex(), f.client.ID.Hex(),
		&domain.SetClientProductAccessDTO{
			Mode:       domain.ProductAccessSelected,
			ProductIDs: []string{f.draft.ID.Hex(), f.h2.ID.Hex()},
		}); err != nil {
		t.Fatalf("SetForClient: %v", err)
	}

	list, err := productSvc.GetAllProducts(ctx, domain.RoleClient, f.client.ID.Hex(), 1, 50, "", nil)
	if err != nil {
		t.Fatalf("GetAllProducts: %v", err)
	}
	if list.Total != 1 || list.Data[0].ID != f.h2.ID {
		t.Errorf("the unpublished product must stay hidden, got total=%d", list.Total)
	}
	if _, err := productSvc.GetProductByID(ctx, domain.RoleClient, f.client.ID.Hex(), f.draft.ID.Hex()); err == nil {
		t.Error("an unpublished product must not open for a client even when granted")
	}
}

func TestTheCatalogFailsClosedWithoutTheAccessService(t *testing.T) {
	// A missing dependency must not become "everyone sees everything" — the one failure nobody
	// would notice in testing.
	f := newAccessFixture()
	productSvc := NewProductService(f.products, nil, nil, nil)

	if _, err := productSvc.GetAllProducts(context.Background(), domain.RoleClient, f.client.ID.Hex(), 1, 50, "", nil); err == nil {
		t.Error("expected the catalog to refuse to answer a client without the access service")
	}
	if _, err := productSvc.GetProductByID(context.Background(), domain.RoleClient, f.client.ID.Hex(), f.h2.ID.Hex()); err == nil {
		t.Error("expected the detail read to refuse too")
	}
}
