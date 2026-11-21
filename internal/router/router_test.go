package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/internal/config"
	"github.com/smart-invest-solutions/backend/internal/database"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestSetupRegistersRoutesWithoutConflicts builds the full router against an unreachable database.
// Gin panics at registration time on conflicting routes (e.g. a new "/:id/mark-paid" clashing with
// an existing wildcard), which would crash the server on boot — this catches that without MongoDB.
func TestSetupRegistersRoutesWithoutConflicts(t *testing.T) {
	gin.SetMode(gin.TestMode)

	client, err := mongo.Connect(options.Client().
		ApplyURI("mongodb://127.0.0.1:1").
		SetServerSelectionTimeout(200 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	db := &database.MongoDB{Client: client, Database: client.Database("router_test")}

	r := Setup(db, &config.Config{JWTSecret: "test", JWTExpiryHours: "1"})

	want := map[string]bool{
		"POST /api/v1/life-insurances/:id/mark-paid":     false,
		"POST /api/v1/health-insurances/:id/mark-paid":   false,
		"GET /api/v1/users":                              false,
		"PUT /api/v1/calculators/settings":               false,
		"GET /api/v1/referrals/my-stats":                 false,
		"GET /api/v1/referrals/all":                      false,
		"GET /api/v1/referrals/summary":                  false,
		"POST /api/v1/agency/sync/postal-report":         false,
		"GET /api/v1/agency/imported-deposits":           false,
		"POST /api/v1/agency/imported-deposits/:id/link": false,
		"GET /api/v1/users/me/advisor":                   false,
		"GET /api/v1/client-map":                         false,
		"GET /api/v1/renewals":                           false,
		"GET /api/v1/announcements":                      false,
		"GET /api/v1/announcements/all":                  false,
		"POST /api/v1/announcements":                     false,
		"PUT /api/v1/announcements/:id":                  false,
		"DELETE /api/v1/announcements/:id":               false,
		"GET /api/v1/admins/next-id":                     false,
		"GET /api/v1/users/:id/product-access":           false,
		"PUT /api/v1/users/:id/product-access":           false,
		"POST /api/v1/users/logout":                      false,
		"GET /api/v1/cron/health":                        false,
		"POST /api/v1/cron/run":                          false,
		"GET /api/v1/cron/run":                           false,
	}
	for _, route := range r.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for key, found := range want {
		if !found {
			t.Errorf("route %s is not registered", key)
		}
	}

	// Protected routes must refuse a request with no token before touching the database.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a token, got %d", rec.Code)
	}
}

func TestAdminLoginRateLimiting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	client, err := mongo.Connect(options.Client().
		ApplyURI("mongodb://127.0.0.1:1").
		SetServerSelectionTimeout(200 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	db := &database.MongoDB{Client: client, Database: client.Database("router_test")}
	r := Setup(db, &config.Config{JWTSecret: "test", JWTExpiryHours: "1"})

	targetIP := "203.0.113.195:54321"

	// 15 requests allowed within 1 minute window
	for i := 1; i <= 15; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admins/login", strings.NewReader(`{"admin_id":"ADM01","pin":"1234"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = targetIP

		r.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was unexpectedly rate limited (429)", i)
		}
	}

	// 16th request from the same IP must be rejected with 429 Too Many Requests
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admins/login", strings.NewReader(`{"admin_id":"ADM01","pin":"1234"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = targetIP

	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 16th request to return 429 Too Many Requests, got %d", rec.Code)
	}

	// Another IP should not be blocked
	otherRec := httptest.NewRecorder()
	otherReq := httptest.NewRequest(http.MethodPost, "/api/v1/admins/login", strings.NewReader(`{"admin_id":"ADM01","pin":"1234"}`))
	otherReq.Header.Set("Content-Type", "application/json")
	otherReq.RemoteAddr = "198.51.100.22:12345"

	r.ServeHTTP(otherRec, otherReq)
	if otherRec.Code == http.StatusTooManyRequests {
		t.Fatalf("different IP was unexpectedly rate limited")
	}
}

func TestResendEmailOTPRateLimiting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	client, err := mongo.Connect(options.Client().
		ApplyURI("mongodb://127.0.0.1:1").
		SetServerSelectionTimeout(200 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	db := &database.MongoDB{Client: client, Database: client.Database("router_test")}
	r := Setup(db, &config.Config{JWTSecret: "test", JWTExpiryHours: "1"})

	targetIP := "203.0.113.196:54321"

	// 5 requests allowed within 1 minute window
	for i := 1; i <= 5; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users/resend-email-otp", strings.NewReader(`{"email":"test@example.com"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = targetIP

		r.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was unexpectedly rate limited (429)", i)
		}
	}

	// 6th request from the same IP must be rejected with 429 Too Many Requests
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/resend-email-otp", strings.NewReader(`{"email":"test@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = targetIP

	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 6th request to return 429 Too Many Requests, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header to be present on 429 response")
	}

	// Another IP should not be blocked
	otherRec := httptest.NewRecorder()
	otherReq := httptest.NewRequest(http.MethodPost, "/api/v1/users/resend-email-otp", strings.NewReader(`{"email":"test@example.com"}`))
	otherReq.Header.Set("Content-Type", "application/json")
	otherReq.RemoteAddr = "198.51.100.23:12345"

	r.ServeHTTP(otherRec, otherReq)
	if otherRec.Code == http.StatusTooManyRequests {
		t.Fatalf("different IP was unexpectedly rate limited")
	}
}

func TestAccessRequestRateLimiting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	client, err := mongo.Connect(options.Client().
		ApplyURI("mongodb://127.0.0.1:1").
		SetServerSelectionTimeout(200 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	db := &database.MongoDB{Client: client, Database: client.Database("router_test")}
	r := Setup(db, &config.Config{JWTSecret: "test", JWTExpiryHours: "1"})

	targetIP := "203.0.113.197:54321"

	// 10 requests allowed within 1 minute window
	for i := 1; i <= 10; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/access-requests", strings.NewReader(`{"name":"Tester","email":"tester@example.com","phone":"9876543210"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = targetIP

		r.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was unexpectedly rate limited (429)", i)
		}
	}

	// 11th request from the same IP must be rejected with 429 Too Many Requests
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/access-requests", strings.NewReader(`{"name":"Tester","email":"tester@example.com","phone":"9876543210"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = targetIP

	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 11th request to return 429 Too Many Requests, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header to be present on 429 response")
	}

	// Another IP should not be blocked
	otherRec := httptest.NewRecorder()
	otherReq := httptest.NewRequest(http.MethodPost, "/api/v1/access-requests", strings.NewReader(`{"name":"Tester","email":"tester@example.com","phone":"9876543210"}`))
	otherReq.Header.Set("Content-Type", "application/json")
	otherReq.RemoteAddr = "198.51.100.24:12345"

	r.ServeHTTP(otherRec, otherReq)
	if otherRec.Code == http.StatusTooManyRequests {
		t.Fatalf("different IP was unexpectedly rate limited")
	}
}
