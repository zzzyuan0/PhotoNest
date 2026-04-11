package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/photonest/photonest/internal/platform/audit"
	"github.com/photonest/photonest/internal/platform/auth"
)

type routeSpec struct {
	Operation      string
	Permission     auth.Permission
	RequireCSRF    bool
	RequireRecent  bool
	RequireLibrary bool
	AuditAction    string
	TargetType     string
}

func (s *Server) secure(spec routeSpec, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, source, err := s.auth.AuthenticateRequest(r)
		if err != nil {
			s.recordAudit(r.Context(), audit.Event{
				Action:     spec.AuditAction,
				Result:     classifyAuthError(err),
				TargetType: spec.TargetType,
				Method:     r.Method,
				Path:       r.URL.Path,
				RemoteAddr: r.RemoteAddr,
				UserAgent:  r.UserAgent(),
				Metadata: map[string]any{
					"operation": spec.Operation,
				},
			})
			s.writeAuthError(w, err)
			return
		}

		principal := auth.Principal{
			Session: session,
			Source:  source,
		}

		if spec.RequireLibrary {
			libraryID, ok := resolveLibraryID(r)
			if !ok {
				if !session.IsAdmin() && len(session.LibraryIDs) == 1 {
					libraryID = session.LibraryIDs[0]
				} else {
					s.writeError(w, http.StatusBadRequest, "library_scope_required", "libraryId is required for this operation", map[string]any{
						"operation": spec.Operation,
					})
					return
				}
			}
			if !session.CanAccessLibrary(libraryID) {
				s.recordAudit(r.Context(), audit.Event{
					Action:     spec.AuditAction,
					Result:     audit.ResultDenied,
					SubjectID:  session.SubjectID,
					SessionID:  session.ID,
					LibraryID:  libraryID,
					TargetType: spec.TargetType,
					Method:     r.Method,
					Path:       r.URL.Path,
					RemoteAddr: r.RemoteAddr,
					UserAgent:  r.UserAgent(),
					Metadata: map[string]any{
						"operation": spec.Operation,
						"reason":    "library_access_denied",
					},
				})
				s.writeError(w, http.StatusForbidden, "forbidden", "subject is not authorized for the requested library", map[string]any{
					"libraryId": libraryID,
					"operation": spec.Operation,
				})
				return
			}
			principal.LibraryID = libraryID
		}

		if !session.HasPermission(spec.Permission) {
			s.recordAudit(r.Context(), audit.Event{
				Action:     spec.AuditAction,
				Result:     audit.ResultDenied,
				SubjectID:  session.SubjectID,
				SessionID:  session.ID,
				LibraryID:  principal.LibraryID,
				TargetType: spec.TargetType,
				Method:     r.Method,
				Path:       r.URL.Path,
				RemoteAddr: r.RemoteAddr,
				UserAgent:  r.UserAgent(),
				Metadata: map[string]any{
					"operation":  spec.Operation,
					"permission": string(spec.Permission),
				},
			})
			s.writeError(w, http.StatusForbidden, "forbidden", "subject lacks the required permission", map[string]any{
				"permission": string(spec.Permission),
				"operation":  spec.Operation,
			})
			return
		}

		if spec.RequireCSRF && s.auth.ShouldEnforceCSRF(r, source) {
			if err := s.auth.ValidateCSRF(r, session); err != nil {
				s.recordAudit(r.Context(), audit.Event{
					Action:     spec.AuditAction,
					Result:     classifyAuthError(err),
					SubjectID:  session.SubjectID,
					SessionID:  session.ID,
					LibraryID:  principal.LibraryID,
					TargetType: spec.TargetType,
					Method:     r.Method,
					Path:       r.URL.Path,
					RemoteAddr: r.RemoteAddr,
					UserAgent:  r.UserAgent(),
					Metadata: map[string]any{
						"operation": spec.Operation,
						"reason":    err.Error(),
					},
				})
				s.writeAuthError(w, err)
				return
			}
		}

		if spec.RequireRecent {
			if err := s.auth.EnsureRecentAuth(session); err != nil {
				s.recordAudit(r.Context(), audit.Event{
					Action:     spec.AuditAction,
					Result:     classifyAuthError(err),
					SubjectID:  session.SubjectID,
					SessionID:  session.ID,
					LibraryID:  principal.LibraryID,
					TargetType: spec.TargetType,
					Method:     r.Method,
					Path:       r.URL.Path,
					RemoteAddr: r.RemoteAddr,
					UserAgent:  r.UserAgent(),
					Metadata: map[string]any{
						"operation": spec.Operation,
					},
				})
				s.writeAuthError(w, err)
				return
			}
		}

		ctx := auth.WithPrincipal(r.Context(), principal)
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next(recorder, r.WithContext(ctx))

		if spec.AuditAction != "" {
			s.recordAudit(ctx, audit.Event{
				Action:     spec.AuditAction,
				Result:     statusToAuditResult(recorder.status),
				SubjectID:  session.SubjectID,
				SessionID:  session.ID,
				LibraryID:  principal.LibraryID,
				TargetType: spec.TargetType,
				TargetID:   resolveTargetID(r),
				Method:     r.Method,
				Path:       r.URL.Path,
				RemoteAddr: r.RemoteAddr,
				UserAgent:  r.UserAgent(),
				Metadata: map[string]any{
					"operation": spec.Operation,
					"status":    recorder.status,
				},
			})
		}
	}
}

func (s *Server) writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrMissingCredentials):
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrInvalidSession), errors.Is(err, auth.ErrSessionExpired):
		s.writeError(w, http.StatusUnauthorized, "invalid_session", "session is invalid or expired", nil)
	case errors.Is(err, auth.ErrCSRFTokenRequired):
		s.writeError(w, http.StatusForbidden, "csrf_token_required", "csrf token is required", map[string]any{
			"header": s.auth.CSRFHeaderName(),
		})
	case errors.Is(err, auth.ErrCSRFTokenInvalid):
		s.writeError(w, http.StatusForbidden, "csrf_token_invalid", "csrf token is invalid", map[string]any{
			"header": s.auth.CSRFHeaderName(),
		})
	case errors.Is(err, auth.ErrRecentAuthRequired):
		s.writeError(w, http.StatusForbidden, "recent_auth_required", "recent authentication is required", map[string]any{
			"window": s.cfg.Security.RecentAuthWindow.String(),
		})
	default:
		s.writeError(w, http.StatusForbidden, "forbidden", "request is not allowed", nil)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, code string, message string, details map[string]any) {
	writeJSON(w, status, ErrorResponse{
		Code:    code,
		Message: message,
		TraceID: code,
		Details: details,
	})
}

func (s *Server) recordAudit(ctx context.Context, event audit.Event) {
	if event.Action == "" {
		return
	}
	s.audit.Record(ctx, event)
}

func resolveLibraryID(r *http.Request) (string, bool) {
	if value := strings.TrimSpace(r.URL.Query().Get("libraryId")); value != "" {
		return value, true
	}
	if value := strings.TrimSpace(r.Header.Get("X-PhotoNest-Library-ID")); value != "" {
		return value, true
	}

	body, ok := snapshotBody(r)
	if !ok || len(body) == 0 {
		return "", false
	}

	var payload map[string]any
	if err := jsonUnmarshal(body, &payload); err != nil {
		return "", false
	}
	value, ok := payload["libraryId"].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}

	return strings.TrimSpace(value), true
}

func resolveTargetID(r *http.Request) string {
	for _, name := range []string{"assetId", "providerName", "runId", "sessionId"} {
		if value := strings.TrimSpace(r.PathValue(name)); value != "" {
			return value
		}
	}

	return ""
}

func snapshotBody(r *http.Request) ([]byte, bool) {
	if r.Body == nil {
		return nil, false
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, true
}

func jsonUnmarshal(data []byte, dest any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dest)
}

func classifyAuthError(err error) audit.Result {
	switch {
	case errors.Is(err, auth.ErrMissingCredentials), errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrInvalidSession), errors.Is(err, auth.ErrSessionExpired):
		return audit.ResultDenied
	case errors.Is(err, auth.ErrCSRFTokenRequired), errors.Is(err, auth.ErrCSRFTokenInvalid), errors.Is(err, auth.ErrRecentAuthRequired):
		return audit.ResultInvalid
	default:
		return audit.ResultError
	}
}

func statusToAuditResult(status int) audit.Result {
	switch {
	case status >= 200 && status < 300:
		return audit.ResultSuccess
	case status == http.StatusNotImplemented:
		return audit.ResultUnimplemented
	case status >= 400 && status < 500:
		return audit.ResultDenied
	default:
		return audit.ResultError
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(payload []byte) (int, error) {
	return r.ResponseWriter.Write(payload)
}
