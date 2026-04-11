package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/photonest/photonest/internal/platform/auth"
	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/platform/health"
)

func TestTimelineRequiresAuthentication(t *testing.T) {
	handler := newTestHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/timeline?libraryId=11111111-1111-1111-1111-111111111111", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
}

func TestLoginThenTimelineSucceeds(t *testing.T) {
	handler := newTestHandler(t)

	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"secret-password"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginRequest)

	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d", loginRecorder.Code)
	}

	cookies := loginRecorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookies to be set")
	}

	timelineRequest := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/timeline?libraryId=11111111-1111-1111-1111-111111111111", nil)
	for _, cookie := range cookies {
		timelineRequest.AddCookie(cookie)
	}

	timelineRecorder := httptest.NewRecorder()
	handler.ServeHTTP(timelineRecorder, timelineRequest)

	if timelineRecorder.Code != http.StatusOK {
		t.Fatalf("expected timeline 200, got %d", timelineRecorder.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(timelineRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode timeline payload: %v", err)
	}
	if payload["libraryId"] != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected library id %v", payload["libraryId"])
	}
}

func TestCookieProtectedWriteRequiresCSRFToken(t *testing.T) {
	handler := newTestHandler(t)

	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"secret-password"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginRequest)

	request := httptest.NewRequest(http.MethodPut, "/api/v1/settings/providers/primary-cos", strings.NewReader(`{"rotateSecrets":true}`))
	request.Header.Set("Content-Type", "application/json")
	for _, cookie := range loginRecorder.Result().Cookies() {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "csrf") {
		t.Fatalf("expected csrf error, got %s", recorder.Body.String())
	}
}

func TestExportRequiresRecentAuthentication(t *testing.T) {
	cfg := newTestConfig()
	handler, err := New(cfg, health.Checker{})
	if err != nil {
		t.Fatalf("new server returned error: %v", err)
	}

	manager, err := auth.NewManager(cfg.Security)
	if err != nil {
		t.Fatalf("new auth manager returned error: %v", err)
	}

	session, _, err := manager.NewSession(auth.Subject{
		ID:         "bootstrap-admin",
		Roles:      []string{"admin"},
		LibraryIDs: []string{"11111111-1111-1111-1111-111111111111"},
	}, "bootstrap-password")
	if err != nil {
		t.Fatalf("new session returned error: %v", err)
	}
	session.RecentAuthAt = session.AuthenticatedAt.Add(-2 * time.Hour)
	token, err := manager.EncodeSession(session)
	if err != nil {
		t.Fatalf("encode session returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/exports", strings.NewReader(`{"libraryId":"11111111-1111-1111-1111-111111111111","scope":"library"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "recent_auth_required") {
		t.Fatalf("expected recent auth error, got %s", recorder.Body.String())
	}
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()

	handler, err := New(newTestConfig(), health.Checker{})
	if err != nil {
		t.Fatalf("new server returned error: %v", err)
	}

	return handler
}

func newTestConfig() config.AppConfig {
	return config.AppConfig{
		Security: config.SecurityConfig{
			CSRFEnabled:      true,
			RecentAuthWindow: 15 * time.Minute,
			Session: config.SessionConfig{
				CookieName:     "photonest_session",
				CSRFCookieName: "photonest_csrf",
				CSRFHeaderName: "X-CSRF-Token",
				SigningKey: config.SecretValue{
					Value: strings.Repeat("s", 32),
				},
				MaxAge:   12 * time.Hour,
				SameSite: "strict",
			},
			BootstrapAuth: config.BootstrapAuthConfig{
				Username: "admin",
				Password: config.SecretValue{
					Value: "secret-password",
				},
				Subject: "bootstrap-admin",
				Roles:   []string{"admin"},
			},
		},
	}
}
