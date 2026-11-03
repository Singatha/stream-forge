package httpserver

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/streamforge/streamforge/docs"
	"github.com/streamforge/streamforge/internal/health"
	"github.com/streamforge/streamforge/internal/platform/web"
)

type RouteRegistrar interface {
	RegisterRoutes(chi.Router)
}

func NewRouter(logger *slog.Logger, healthService *health.Service, registry *prometheus.Registry, registrars ...RouteRegistrar) http.Handler {
	router := chi.NewRouter()
	metrics := NewHTTPMetrics(registry)

	router.Use(RequestID)
	router.Use(AccessLog(logger))
	router.Use(metrics.Middleware)
	router.Use(chimiddleware.Recoverer)

	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		web.WriteJSON(w, http.StatusOK, healthService.Live())
	})
	router.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		check, ready := healthService.Ready(r.Context())
		if !ready {
			web.WriteJSON(w, http.StatusServiceUnavailable, check)
			return
		}
		web.WriteJSON(w, http.StatusOK, check)
	})
	router.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	router.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(docs.OpenAPI)
	})
	router.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/", http.StatusPermanentRedirect)
	})
	router.Get("/docs/", swaggerUI)
	router.Route("/api/v1", func(apiRouter chi.Router) {
		for _, registrar := range registrars {
			registrar.RegisterRoutes(apiRouter)
		}
	})

	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		web.WriteError(w, http.StatusNotFound, "ROUTE_NOT_FOUND", "The requested route was not found")
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		web.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "The HTTP method is not allowed for this route")
	})

	return router
}

func swaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html><head><title>StreamForge API</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"></head>
<body><div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>SwaggerUIBundle({url:'/openapi.yaml',dom_id:'#swagger-ui'});</script></body></html>`))
}
