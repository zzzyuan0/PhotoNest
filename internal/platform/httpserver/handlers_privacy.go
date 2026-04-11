package httpserver

import (
	"net/http"
	"strings"

	"github.com/photonest/photonest/internal/library"
	"github.com/photonest/photonest/internal/platform/auth"
)

type updatePrivacyPolicyRequest struct {
	GPSMode       string `json:"gpsMode"`
	OCRMode       string `json:"ocrMode"`
	CaptionMode   string `json:"captionMode"`
	EmbeddingMode string `json:"embeddingMode"`
}

type privacyPolicyResponse struct {
	GPSMode       string `json:"gpsMode"`
	OCRMode       string `json:"ocrMode"`
	CaptionMode   string `json:"captionMode"`
	EmbeddingMode string `json:"embeddingMode"`
}

func (s *Server) handleUpdatePrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.ingestion == nil {
		s.writeError(w, http.StatusServiceUnavailable, "privacy_policy_unavailable", "ingestion service is not configured", nil)
		return
	}

	var request updatePrivacyPolicyRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON", nil)
		return
	}

	policy, err := buildPrivacyPolicy(request)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := s.ingestion.Repository().SaveLibraryPolicy(r.Context(), principal.LibraryID, policy); err != nil {
		s.writeError(w, http.StatusInternalServerError, "privacy_policy_failed", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, privacyPolicyResponse{
		GPSMode:       string(policy.GPSMode),
		OCRMode:       string(policy.OCRMode),
		CaptionMode:   string(policy.CaptionMode),
		EmbeddingMode: string(policy.EmbeddingMode),
	})
}

func buildPrivacyPolicy(request updatePrivacyPolicyRequest) (library.Policy, error) {
	policy := library.DefaultPolicy()

	switch mode := library.GPSMode(strings.TrimSpace(request.GPSMode)); mode {
	case library.GPSModeHidden:
		policy.GPSMode = mode
		policy.AllowGPSPersistence = false
	case library.GPSModeOwnerOnly:
		policy.GPSMode = mode
		policy.AllowGPSPersistence = true
	default:
		return library.Policy{}, errInvalidEnum("gpsMode")
	}

	switch mode := library.TextMode(strings.TrimSpace(request.OCRMode)); mode {
	case library.TextModeHidden:
		policy.OCRMode = mode
		policy.AllowRemoteOCR = false
	case library.TextModePreview, library.TextModeFull:
		policy.OCRMode = mode
		policy.AllowRemoteOCR = true
	default:
		return library.Policy{}, errInvalidEnum("ocrMode")
	}

	switch mode := library.TextMode(strings.TrimSpace(request.CaptionMode)); mode {
	case library.TextModeHidden:
		policy.CaptionMode = mode
		policy.AllowRemoteCaption = false
	case library.TextModePreview, library.TextModeFull:
		policy.CaptionMode = mode
		policy.AllowRemoteCaption = true
	default:
		return library.Policy{}, errInvalidEnum("captionMode")
	}

	switch mode := library.EmbeddingMode(strings.TrimSpace(request.EmbeddingMode)); mode {
	case library.EmbeddingModeDisabled:
		policy.EmbeddingMode = mode
		policy.AllowRemoteEmbedding = false
	case library.EmbeddingModePrivate:
		policy.EmbeddingMode = mode
		policy.AllowRemoteEmbedding = true
	default:
		return library.Policy{}, errInvalidEnum("embeddingMode")
	}

	return policy, nil
}

func errInvalidEnum(field string) error {
	return &fieldError{field: field}
}

type fieldError struct {
	field string
}

func (e *fieldError) Error() string {
	return e.field + " contains an unsupported value"
}
