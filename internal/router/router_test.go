package router

import (
	"net/http"
	"net/http/httptest"
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
		"POST /api/v1/life-insurances/:id/mark-paid":   false,
		"POST /api/v1/health-insurances/:id/mark-paid": false,
		"GET /api/v1/users":                            false,
		"PUT /api/v1/calculators/settings":             false,
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
