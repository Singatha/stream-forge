package httpserver

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/streamforge/streamforge/internal/platform/web"
)

type rateWindow struct {
	started  time.Time
	lastSeen time.Time
	count    int
}

type IPRateLimiter struct {
	mu      sync.Mutex
	clients map[string]rateWindow
	limit   int
	window  time.Duration
	calls   uint64
	now     func() time.Time
}

func NewIPRateLimiter(limit int, window time.Duration) *IPRateLimiter {
	return &IPRateLimiter{clients: make(map[string]rateWindow), limit: limit, window: window, now: time.Now}
}

func (l *IPRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed, remaining, retryAfter := l.allow(clientIP(r.RemoteAddr))
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(l.limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		if !allowed {
			seconds := int(math.Ceil(retryAfter.Seconds()))
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			web.WriteError(w, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "Too many requests; try again later")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *IPRateLimiter) allow(key string) (bool, int, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	entry, exists := l.clients[key]
	if !exists || now.Sub(entry.started) >= l.window {
		entry = rateWindow{started: now}
	}
	entry.lastSeen = now
	entry.count++
	l.clients[key] = entry
	l.calls++
	if l.calls%256 == 0 {
		for client, candidate := range l.clients {
			if now.Sub(candidate.lastSeen) >= 2*l.window {
				delete(l.clients, client)
			}
		}
	}

	remaining := l.limit - entry.count
	if remaining < 0 {
		remaining = 0
	}
	if entry.count > l.limit {
		return false, remaining, l.window - now.Sub(entry.started)
	}
	return true, remaining, 0
}

func clientIP(remoteAddress string) string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil {
		return host
	}
	return remoteAddress
}
