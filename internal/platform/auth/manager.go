package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/photonest/photonest/internal/platform/config"
)

var (
	ErrMissingCredentials  = errors.New("missing credentials")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrInvalidSession      = errors.New("invalid session")
	ErrSessionExpired      = errors.New("session expired")
	ErrCSRFTokenRequired   = errors.New("csrf token is required")
	ErrCSRFTokenInvalid    = errors.New("csrf token is invalid")
	ErrRecentAuthRequired  = errors.New("recent authentication required")
	ErrReauthNotSupported  = errors.New("reauthentication is not supported for this session")
)

type Manager struct {
	cookieName     string
	csrfCookieName string
	csrfHeaderName string
	csrfEnabled    bool
	maxAge         time.Duration
	recentWindow   time.Duration
	secureCookies  bool
	sameSite       http.SameSite
	signingKey     []byte
	bootstrap      bootstrapCredentials
	now            func() time.Time
}

type bootstrapCredentials struct {
	Username string
	Password string
	Subject  Subject
}

func NewManager(cfg config.SecurityConfig) (*Manager, error) {
	signingKey, err := cfg.Session.SigningKey.Resolve(context.Background(), config.ResolveOptions{})
	if err != nil {
		return nil, fmt.Errorf("resolve signing key: %w", err)
	}
	password, err := cfg.BootstrapAuth.Password.Resolve(context.Background(), config.ResolveOptions{})
	if err != nil {
		return nil, fmt.Errorf("resolve bootstrap password: %w", err)
	}

	return &Manager{
		cookieName:     cfg.Session.CookieName,
		csrfCookieName: cfg.Session.CSRFCookieName,
		csrfHeaderName: cfg.Session.CSRFHeaderName,
		csrfEnabled:    cfg.CSRFEnabled,
		maxAge:         cfg.Session.MaxAge,
		recentWindow:   cfg.RecentAuthWindow,
		secureCookies:  cfg.Session.SecureCookies,
		sameSite:       parseSameSite(cfg.Session.SameSite),
		signingKey:     []byte(signingKey),
		bootstrap: bootstrapCredentials{
			Username: cfg.BootstrapAuth.Username,
			Password: password,
			Subject: Subject{
				ID:          cfg.BootstrapAuth.Subject,
				DisplayName: cfg.BootstrapAuth.DisplayName,
				Roles:       cleanStrings(cfg.BootstrapAuth.Roles),
				LibraryIDs:  cleanStrings(cfg.BootstrapAuth.LibraryIDs),
			},
		},
		now: time.Now,
	}, nil
}

func (m *Manager) Login(username string, password string) (Session, string, error) {
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(username)), []byte(strings.TrimSpace(m.bootstrap.Username))) != 1 {
		return Session{}, "", ErrInvalidCredentials
	}
	if subtle.ConstantTimeCompare([]byte(password), []byte(m.bootstrap.Password)) != 1 {
		return Session{}, "", ErrInvalidCredentials
	}

	return m.NewSession(m.bootstrap.Subject, "bootstrap-password")
}

func (m *Manager) RefreshRecentAuth(session Session, password string) (Session, string, error) {
	if session.AuthMethod != "bootstrap-password" {
		return Session{}, "", ErrReauthNotSupported
	}
	if subtle.ConstantTimeCompare([]byte(password), []byte(m.bootstrap.Password)) != 1 {
		return Session{}, "", ErrInvalidCredentials
	}

	session.RecentAuthAt = m.now().UTC()
	session.ExpiresAt = session.RecentAuthAt.Add(m.maxAge)

	token, err := m.EncodeSession(session)
	if err != nil {
		return Session{}, "", err
	}

	return session, token, nil
}

func (m *Manager) NewSession(subject Subject, authMethod string) (Session, string, error) {
	now := m.now().UTC()

	session := Session{
		ID:              randomToken(16),
		SubjectID:       strings.TrimSpace(subject.ID),
		DisplayName:     strings.TrimSpace(subject.DisplayName),
		Roles:           cleanStrings(subject.Roles),
		LibraryIDs:      cleanStrings(subject.LibraryIDs),
		AuthMethod:      strings.TrimSpace(authMethod),
		AuthenticatedAt: now,
		RecentAuthAt:    now,
		ExpiresAt:       now.Add(m.maxAge),
		CSRFToken:       randomToken(24),
	}

	token, err := m.EncodeSession(session)
	if err != nil {
		return Session{}, "", err
	}

	return session, token, nil
}

func (m *Manager) EncodeSession(session Session) (string, error) {
	payload, err := json.Marshal(session)
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}

	payloadPart := base64.RawURLEncoding.EncodeToString(payload)
	signature := m.sign(payloadPart)
	return payloadPart + "." + signature, nil
}

func (m *Manager) DecodeSession(token string) (Session, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 2 {
		return Session{}, ErrInvalidSession
	}

	payloadPart := parts[0]
	signature := m.sign(payloadPart)
	if subtle.ConstantTimeCompare([]byte(signature), []byte(parts[1])) != 1 {
		return Session{}, ErrInvalidSession
	}

	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return Session{}, ErrInvalidSession
	}

	var session Session
	if err := json.Unmarshal(payload, &session); err != nil {
		return Session{}, ErrInvalidSession
	}
	if session.ExpiresAt.Before(m.now().UTC()) {
		return Session{}, ErrSessionExpired
	}

	return session, nil
}

func (m *Manager) AuthenticateRequest(r *http.Request) (Session, Source, error) {
	if token := bearerToken(r.Header.Get("Authorization")); token != "" {
		session, err := m.DecodeSession(token)
		if err != nil {
			return Session{}, SourceBearer, err
		}
		return session, SourceBearer, nil
	}

	cookie, err := r.Cookie(m.cookieName)
	if err == nil && strings.TrimSpace(cookie.Value) != "" {
		session, decodeErr := m.DecodeSession(cookie.Value)
		if decodeErr != nil {
			return Session{}, SourceCookie, decodeErr
		}
		return session, SourceCookie, nil
	}

	return Session{}, SourceNone, ErrMissingCredentials
}

func (m *Manager) ShouldEnforceCSRF(r *http.Request, source Source) bool {
	if !m.csrfEnabled || source != SourceCookie {
		return false
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func (m *Manager) ValidateCSRF(r *http.Request, session Session) error {
	token := strings.TrimSpace(r.Header.Get(m.csrfHeaderName))
	if token == "" {
		return ErrCSRFTokenRequired
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(session.CSRFToken)) != 1 {
		return ErrCSRFTokenInvalid
	}

	return nil
}

func (m *Manager) EnsureRecentAuth(session Session) error {
	if session.RecentAuthAt.Add(m.recentWindow).Before(m.now().UTC()) {
		return ErrRecentAuthRequired
	}

	return nil
}

func (m *Manager) SetSessionCookies(w http.ResponseWriter, session Session, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secureCookies,
		SameSite: m.sameSite,
		Expires:  session.ExpiresAt,
		MaxAge:   int(time.Until(session.ExpiresAt).Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     m.csrfCookieName,
		Value:    session.CSRFToken,
		Path:     "/",
		HttpOnly: false,
		Secure:   m.secureCookies,
		SameSite: m.sameSite,
		Expires:  session.ExpiresAt,
		MaxAge:   int(time.Until(session.ExpiresAt).Seconds()),
	})
}

func (m *Manager) ClearSessionCookies(w http.ResponseWriter) {
	expired := time.Unix(0, 0).UTC()
	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secureCookies,
		SameSite: m.sameSite,
		Expires:  expired,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     m.csrfCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		Secure:   m.secureCookies,
		SameSite: m.sameSite,
		Expires:  expired,
		MaxAge:   -1,
	})
}

func (m *Manager) SessionCookieName() string {
	return m.cookieName
}

func (m *Manager) CSRFCookieName() string {
	return m.csrfCookieName
}

func (m *Manager) CSRFHeaderName() string {
	return m.csrfHeaderName
}

func (m *Manager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.signingKey)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func bearerToken(value string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return ""
	}

	return strings.TrimSpace(strings.TrimPrefix(value, prefix))
}

func parseSameSite(value string) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "lax":
		return http.SameSiteLaxMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteStrictMode
	}
}

func cleanStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, trimmed)
	}

	return cleaned
}

func randomToken(size int) string {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		panic(fmt.Sprintf("generate random token: %v", err))
	}

	return hex.EncodeToString(buffer)
}
