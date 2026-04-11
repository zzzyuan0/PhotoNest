package httpserver

import (
	"net/http"
	"time"

	"github.com/photonest/photonest/internal/platform/auth"
)

type providerRunDebugResponse struct {
	RunID        string         `json:"runId"`
	Status       string         `json:"status"`
	DebugPayload map[string]any `json:"debugPayload,omitempty"`
}

func (s *Server) handleGetProviderRunDebug(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.enrich == nil {
		s.writeError(w, http.StatusServiceUnavailable, "debug_unavailable", "enrichment service is not configured", nil)
		return
	}

	run, found, err := s.enrich.GetRecognitionRun(r.Context(), r.PathValue("runId"))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "debug_lookup_failed", err.Error(), nil)
		return
	}
	if !found {
		s.writeError(w, http.StatusNotFound, "debug_not_found", "provider run was not found", nil)
		return
	}
	if run.DebugExpiresAt != nil && run.DebugExpiresAt.Before(s.enrichNow()) {
		s.writeError(w, http.StatusNotFound, "debug_not_found", "provider run debug payload has expired", nil)
		return
	}

	writeJSON(w, http.StatusOK, providerRunDebugResponse{
		RunID:        run.ID,
		Status:       string(run.Status),
		DebugPayload: run.DebugPayload,
	})
}

func (s *Server) enrichNow() time.Time {
	return time.Now().UTC()
}
