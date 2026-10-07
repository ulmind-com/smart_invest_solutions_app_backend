package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smart-invest-solutions/backend/pkg/response"
)

// RateLimit caps how often one client IP may call a public, unauthenticated endpoint.
//
// It exists for the endpoints whose answer is itself worth something to an attacker. Password reset
// tells the caller whether an address is registered — a deliberate product choice, so that someone
// who mistyped their email is told rather than left waiting — and without a limit that answer would
// be a free tool for testing addresses in bulk. A few requests a minute is far more than a person
// resetting their own password ever needs, and far less than enumeration needs to be worthwhile.
//
// The counters live in memory, which is the right scope for a single-process deployment: it is a
// speed bump on one process, not a distributed quota. Behind several instances each would allow the
// quota separately, so a shared store would be the next step if this ever runs replicated.
func RateLimit(limit int, window time.Duration) gin.HandlerFunc {
	if limit < 1 {
		limit = 1
	}
	limiter := newIPLimiter(limit, window)

	return func(c *gin.Context) {
		if retryAfter, ok := limiter.allow(c.ClientIP(), time.Now()); !ok {
			// Stated in seconds so the app can tell the user how long to wait rather than just
			// failing — and no detail about what was being looked up.
			c.Header("Retry-After", retryAfterSeconds(retryAfter))
			response.Error(c, http.StatusTooManyRequests,
				"Too many attempts from this device. Wait a minute and try again.")
			c.Abort()
			return
		}
		c.Next()
	}
}

// ipLimiter is a fixed-window counter per client IP.
type ipLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	// windows holds each IP's current window: when it started and how many requests it has seen.
	windows map[string]*ipWindow
	// lastSweep throttles the cleanup below, so a long-running process doesn't grow a map entry per
	// IP it has ever seen.
	lastSweep time.Time
}

type ipWindow struct {
	startedAt time.Time
	count     int
}

func newIPLimiter(limit int, window time.Duration) *ipLimiter {
	return &ipLimiter{
		limit:     limit,
		window:    window,
		windows:   make(map[string]*ipWindow),
		lastSweep: time.Now(),
	}
}

// allow records a request and reports whether it is within the quota, plus how long until the
// current window resets.
func (l *ipLimiter) allow(ip string, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweep(now)

	current, seen := l.windows[ip]
	if !seen || now.Sub(current.startedAt) >= l.window {
		l.windows[ip] = &ipWindow{startedAt: now, count: 1}
		return 0, true
	}

	if current.count >= l.limit {
		return l.window - now.Sub(current.startedAt), false
	}

	current.count++
	return 0, true
}

// sweep drops windows that have expired. Called under the lock, at most once per window.
func (l *ipLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.window {
		return
	}
	for ip, w := range l.windows {
		if now.Sub(w.startedAt) >= l.window {
			delete(l.windows, ip)
		}
	}
	l.lastSweep = now
}

// retryAfterSeconds renders a Retry-After header value, always at least one second so a client that
// honours it actually waits.
func retryAfterSeconds(d time.Duration) string {
	seconds := int(d.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}
