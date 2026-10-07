package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func rateLimitedRouter(limit int, window time.Duration) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/forgot", RateLimit(limit, window), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func callFrom(r *gin.Engine, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/forgot", nil)
	// Gin derives ClientIP from RemoteAddr when no trusted proxy header is set.
	req.RemoteAddr = ip + ":54321"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestRateLimitAllowsTheQuotaThenRefuses(t *testing.T) {
	r := rateLimitedRouter(3, time.Minute)

	for i := 1; i <= 3; i++ {
		if code := callFrom(r, "203.0.113.5").Code; code != http.StatusOK {
			t.Fatalf("request %d should be allowed, got %d", i, code)
		}
	}

	refused := callFrom(r, "203.0.113.5")
	if refused.Code != http.StatusTooManyRequests {
		t.Fatalf("the fourth request should be refused, got %d", refused.Code)
	}
	// A client that honours Retry-After needs a usable value, never "0".
	if retry := refused.Header().Get("Retry-After"); retry == "" || retry == "0" {
		t.Errorf("Retry-After = %q, want a positive number of seconds", retry)
	}
	// The refusal must not say what was being looked up.
	if body := refused.Body.String(); !strings.Contains(body, "Too many attempts") {
		t.Errorf("unexpected refusal body: %s", body)
	}
}

func TestRateLimitIsPerClient(t *testing.T) {
	// One person hammering the form must not lock everyone else out of resetting their password.
	r := rateLimitedRouter(2, time.Minute)

	for i := 0; i < 3; i++ {
		callFrom(r, "203.0.113.5")
	}
	if code := callFrom(r, "203.0.113.6").Code; code != http.StatusOK {
		t.Errorf("a different client should still be allowed, got %d", code)
	}
}

func TestRateLimitWindowReopens(t *testing.T) {
	r := rateLimitedRouter(1, 40*time.Millisecond)

	if code := callFrom(r, "203.0.113.7").Code; code != http.StatusOK {
		t.Fatalf("the first request should be allowed, got %d", code)
	}
	if code := callFrom(r, "203.0.113.7").Code; code != http.StatusTooManyRequests {
		t.Fatalf("the second request in the window should be refused, got %d", code)
	}

	time.Sleep(60 * time.Millisecond)
	if code := callFrom(r, "203.0.113.7").Code; code != http.StatusOK {
		t.Errorf("the window should have reopened, got %d", code)
	}
}

func TestRateLimitForgetsClientsOnceTheirWindowPasses(t *testing.T) {
	// A long-running process must not grow one map entry per IP it has ever seen.
	limiter := newIPLimiter(1, 20*time.Millisecond)
	start := time.Now()

	for i := 0; i < 50; i++ {
		limiter.allow(ipFor(i), start)
	}
	if len(limiter.windows) != 50 {
		t.Fatalf("expected 50 tracked clients, got %d", len(limiter.windows))
	}

	// One call a full window later sweeps the expired entries.
	limiter.allow("203.0.113.1", start.Add(time.Second))
	if len(limiter.windows) > 1 {
		t.Errorf("expired windows should have been swept, %d left", len(limiter.windows))
	}
}

func TestRateLimitTreatsAZeroLimitAsOne(t *testing.T) {
	// A misconfigured limit must not mean "block everything" — one request still gets through.
	r := rateLimitedRouter(0, time.Minute)
	if code := callFrom(r, "203.0.113.9").Code; code != http.StatusOK {
		t.Errorf("the first request should still be allowed, got %d", code)
	}
	if code := callFrom(r, "203.0.113.9").Code; code != http.StatusTooManyRequests {
		t.Errorf("the second should be refused, got %d", code)
	}
}

// ipFor is a distinct address per iteration, so each one gets its own window.
func ipFor(n int) string {
	return fmt.Sprintf("198.51.100.%d", n+1)
}
