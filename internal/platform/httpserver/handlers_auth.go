package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/photonest/photonest/internal/platform/audit"
	"github.com/photonest/photonest/internal/platform/auth"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type refreshRecentAuthRequest struct {
	Password string `json:"password"`
}

type authSessionResponse struct {
	Session     auth.Session `json:"session"`
	AccessToken string       `json:"accessToken,omitempty"`
	CSRFToken   string       `json:"csrfToken,omitempty"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.recordAudit(r.Context(), audit.Event{
			Action:     "auth.login",
			Result:     audit.ResultInvalid,
			TargetType: "session",
			Method:     r.Method,
			Path:       r.URL.Path,
			RemoteAddr: r.RemoteAddr,
			UserAgent:  r.UserAgent(),
			Metadata: map[string]any{
				"reason": "invalid_json",
			},
		})
		s.writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON", nil)
		return
	}

	session, token, err := s.auth.Login(request.Username, request.Password)
	if err != nil {
		result := audit.ResultDenied
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			result = audit.ResultError
		}
		s.recordAudit(r.Context(), audit.Event{
			Action:     "auth.login",
			Result:     result,
			TargetType: "session",
			Method:     r.Method,
			Path:       r.URL.Path,
			RemoteAddr: r.RemoteAddr,
			UserAgent:  r.UserAgent(),
		})
		if errors.Is(err, auth.ErrInvalidCredentials) {
			s.writeError(w, http.StatusUnauthorized, "invalid_credentials", "username or password is incorrect", nil)
			return
		}
		s.writeError(w, http.StatusInternalServerError, "login_failed", "failed to create session", nil)
		return
	}

	s.auth.SetSessionCookies(w, session, token)
	s.recordAudit(r.Context(), audit.Event{
		Action:     "auth.login",
		Result:     audit.ResultSuccess,
		SubjectID:  session.SubjectID,
		SessionID:  session.ID,
		TargetType: "session",
		Method:     r.Method,
		Path:       r.URL.Path,
		RemoteAddr: r.RemoteAddr,
		UserAgent:  r.UserAgent(),
	})
	writeJSON(w, http.StatusOK, authSessionResponse{
		Session:     session,
		AccessToken: token,
		CSRFToken:   session.CSRFToken,
	})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}

	writeJSON(w, http.StatusOK, authSessionResponse{
		Session:   principal.Session,
		CSRFToken: principal.Session.CSRFToken,
	})
}

func (s *Server) handleRefreshRecentAuth(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}

	var request refreshRecentAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON", nil)
		return
	}

	session, token, err := s.auth.RefreshRecentAuth(principal.Session, request.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			s.writeError(w, http.StatusUnauthorized, "invalid_credentials", "password is incorrect", nil)
		case errors.Is(err, auth.ErrReauthNotSupported):
			s.writeError(w, http.StatusForbidden, "reauth_not_supported", "this session cannot be reauthenticated with a password", nil)
		default:
			s.writeError(w, http.StatusInternalServerError, "reauth_failed", "failed to refresh recent authentication", nil)
		}
		return
	}

	s.auth.SetSessionCookies(w, session, token)
	writeJSON(w, http.StatusOK, authSessionResponse{
		Session:     session,
		AccessToken: token,
		CSRFToken:   session.CSRFToken,
	})
}
