package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/streamforge/streamforge/internal/health"
)

type testPinger struct{ err error }

func (p testPinger) Ping(context.Context) error { return p.err }

func TestProbes(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		pingError  error
		wantStatus int
		wantBody   string
	}{
		{name: "liveness", path: "/health", pingError: errors.New("down"), wantStatus: http.StatusOK, wantBody: `"status":"ok"`},
		{name: "ready", path: "/ready", wantStatus: http.StatusOK, wantBody: `"postgres":"ok"`},
		{name: "not ready", path: "/ready", pingError: errors.New("down"), wantStatus: http.StatusServiceUnavailable, wantBody: `"postgres":"unavailable"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), health.NewService(testPinger{tt.pingError}, time.Second), prometheus.NewRegistry())
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if !strings.Contains(response.Body.String(), tt.wantBody) {
				t.Fatalf("body = %q, want fragment %q", response.Body.String(), tt.wantBody)
			}
			if response.Header().Get("X-Request-ID") == "" {
				t.Fatal("X-Request-ID header is empty")
			}
		})
	}
}

func TestUnknownRouteUsesErrorEnvelope(t *testing.T) {
	router := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), health.NewService(testPinger{}, time.Second), prometheus.NewRegistry())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"ROUTE_NOT_FOUND"`) {
		t.Fatalf("unexpected response: status=%d body=%s", response.Code, response.Body.String())
	}
}
