package httpserver

import (
	"encoding/json"
	"net/http"

	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/platform/health"
)

type Server struct {
	cfg     config.AppConfig
	checker health.Checker
	mux     *http.ServeMux
}

func New(cfg config.AppConfig, checker health.Checker) http.Handler {
	server := &Server{
		cfg:     cfg,
		checker: checker,
		mux:     http.NewServeMux(),
	}

	server.routes()

	return server.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/v1/health", s.handleHealth)
	s.mux.HandleFunc("/api/v1/import/sessions", s.notImplemented("createImportSession"))
	s.mux.HandleFunc("/api/v1/discovery/timeline", s.notImplemented("listTimelineAssets"))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	results := s.checker.Check(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"status": s.healthStatus(results),
		"checks": results,
	})
}

func (s *Server) healthStatus(results []health.Status) string {
	return health.OverallStatus(results)
}

func (s *Server) notImplemented(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotImplemented, ErrorResponse{
			Code:    "not_implemented",
			Message: "operation is scaffolded but not implemented yet",
			TraceID: operation,
			Details: map[string]any{
				"operation": operation,
				"method":    r.Method,
				"path":      r.URL.Path,
			},
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
