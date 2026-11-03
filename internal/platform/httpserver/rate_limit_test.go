package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIPRateLimiter(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	limiter := NewIPRateLimiter(2, time.Minute)
	limiter.now = func() time.Time { return now }
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	for attempt, expected := range []int{http.StatusNoContent, http.StatusNoContent, http.StatusTooManyRequests} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/login", nil)
		request.RemoteAddr = "192.0.2.1:1234"
		handler.ServeHTTP(response, request)
		if response.Code != expected {
			t.Fatalf("attempt %d status = %d, want %d", attempt+1, response.Code, expected)
		}
	}

	now = now.Add(time.Minute)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/login", nil)
	request.RemoteAddr = "192.0.2.1:9999"
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status after reset = %d", response.Code)
	}
}
