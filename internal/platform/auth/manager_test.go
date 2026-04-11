package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/photonest/photonest/internal/platform/config"
)

func TestManagerLoginAndAuthenticateBearer(t *testing.T) {
	manager := newTestManager(t)

	session, token, err := manager.Login("admin", "secret-password")
	if err != nil {
		t.Fatalf("login returned error: %v", err)
	}
	if session.SubjectID != "bootstrap-admin" {
		t.Fatalf("unexpected subject id %q", session.SubjectID)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	request.Header.Set("Authorization", "Bearer "+token)

	got, source, err := manager.AuthenticateRequest(request)
	if err != nil {
		t.Fatalf("authenticate request returned error: %v", err)
	}
	if source != SourceBearer {
		t.Fatalf("expected bearer source, got %q", source)
	}
	if got.ID != session.ID {
		t.Fatalf("expected session %q, got %q", session.ID, got.ID)
	}
}

func TestManagerValidateCSRFAndRecentAuth(t *testing.T) {
	manager := newTestManager(t)
	baseTime := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return baseTime }

	session, _, err := manager.NewSession(Subject{
		ID:    "bootstrap-admin",
		Roles: []string{"admin"},
	}, "bootstrap-password")
	if err != nil {
		t.Fatalf("new session returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/exports", nil)
	request.Header.Set(manager.CSRFHeaderName(), session.CSRFToken)
	if err := manager.ValidateCSRF(request, session); err != nil {
		t.Fatalf("validate csrf returned error: %v", err)
	}

	manager.now = func() time.Time { return baseTime.Add(2 * time.Hour) }
	if err := manager.EnsureRecentAuth(session); err == nil {
		t.Fatal("expected recent auth error, got nil")
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()

	manager, err := NewManager(config.SecurityConfig{
		CSRFEnabled:      true,
		RecentAuthWindow: 15 * time.Minute,
		Session: config.SessionConfig{
			CookieName:     "photonest_session",
			CSRFCookieName: "photonest_csrf",
			CSRFHeaderName: "X-CSRF-Token",
			SigningKey: config.SecretValue{
				Value: strings.Repeat("x", 32),
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
	})
	if err != nil {
		t.Fatalf("new manager returned error: %v", err)
	}

	return manager
}
