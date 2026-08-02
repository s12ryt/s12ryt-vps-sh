package panel

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vps-sh/internal/auth"
)

func TestCredentialRevealRequiresBoundSessionCSRFAndPassword(t *testing.T) {
	server, _ := newCredentialRevealServer(t)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")
	path := "/api/nodes/existing-vless/credential"
	secret := "11111111-1111-4111-8111-111111111111"

	missingCSRF := performCredentialRequest(t, server, path, cookie, "", "198.51.100.8", []byte(`{"password":"panel-password"}`))
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", missingCSRF.Code)
	}
	differentClient := performCredentialRequest(t, server, path, cookie, csrfToken, "203.0.113.9", []byte(`{"password":"panel-password"}`))
	if differentClient.Code != http.StatusUnauthorized {
		t.Fatalf("different client status = %d, want 401", differentClient.Code)
	}
	wrongPassword := performCredentialRequest(t, server, path, cookie, csrfToken, "198.51.100.8", []byte(`{"password":"wrong"}`))
	if wrongPassword.Code != http.StatusUnauthorized || strings.Contains(wrongPassword.Body.String(), secret) {
		t.Fatalf("wrong password response = %d %s", wrongPassword.Code, wrongPassword.Body.String())
	}
	unknownField := performCredentialRequest(t, server, path, cookie, csrfToken, "198.51.100.8", []byte(`{"password":"panel-password","credential":true}`))
	if unknownField.Code != http.StatusBadRequest || strings.Contains(unknownField.Body.String(), secret) {
		t.Fatalf("unknown field response = %d %s", unknownField.Code, unknownField.Body.String())
	}

	revealed := performCredentialRequest(t, server, path, cookie, csrfToken, "198.51.100.8", []byte(`{"password":"panel-password"}`))
	if revealed.Code != http.StatusOK {
		t.Fatalf("reveal status = %d, want 200: %s", revealed.Code, revealed.Body.String())
	}
	for _, expected := range []string{`"uuid":"` + secret + `"`, `"expires_in_seconds":300`} {
		if !strings.Contains(revealed.Body.String(), expected) {
			t.Fatalf("reveal response missing %q: %s", expected, revealed.Body.String())
		}
	}
}

func TestCredentialRevealElevationIsSessionScopedAndExpiresAfterFiveMinutes(t *testing.T) {
	server, now := newCredentialRevealServer(t)
	firstCookie, firstCSRF := authenticatedSession(t, server, "198.51.100.8")
	path := "/api/nodes/existing-vless/credential"

	initial := performCredentialRequest(t, server, path, firstCookie, firstCSRF, "198.51.100.8", []byte(`{"password":"panel-password"}`))
	if initial.Code != http.StatusOK {
		t.Fatalf("initial reveal status = %d, want 200", initial.Code)
	}

	*now = now.Add(4*time.Minute + 59*time.Second)
	withinWindow := performCredentialRequest(t, server, path, firstCookie, firstCSRF, "198.51.100.8", []byte(`{}`))
	if withinWindow.Code != http.StatusOK {
		t.Fatalf("within elevation window status = %d, want 200", withinWindow.Code)
	}

	secondCookie, secondCSRF := authenticatedSession(t, server, "203.0.113.9")
	otherSession := performCredentialRequest(t, server, path, secondCookie, secondCSRF, "203.0.113.9", []byte(`{}`))
	if otherSession.Code != http.StatusUnauthorized {
		t.Fatalf("other session status = %d, want 401", otherSession.Code)
	}

	*now = now.Add(time.Second)
	expired := performCredentialRequest(t, server, path, firstCookie, firstCSRF, "198.51.100.8", []byte(`{}`))
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("expired elevation status = %d, want 401", expired.Code)
	}
}

func TestLogoutDoesNotCarryCredentialElevationIntoNewSession(t *testing.T) {
	server, _ := newCredentialRevealServer(t)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")
	path := "/api/nodes/existing-vless/credential"

	revealed := performCredentialRequest(t, server, path, cookie, csrfToken, "198.51.100.8", []byte(`{"password":"panel-password"}`))
	if revealed.Code != http.StatusOK {
		t.Fatalf("initial reveal status = %d, want 200", revealed.Code)
	}
	logout := performConfigRequest(t, server, http.MethodPost, "/logout", cookie, csrfToken, "", nil)
	if logout.Code != http.StatusSeeOther {
		t.Fatalf("logout status = %d, want 303", logout.Code)
	}

	newCookie, newCSRF := authenticatedSession(t, server, "198.51.100.8")
	afterLogin := performCredentialRequest(t, server, path, newCookie, newCSRF, "198.51.100.8", []byte(`{}`))
	if afterLogin.Code != http.StatusUnauthorized {
		t.Fatalf("new session inherited elevation, status = %d", afterLogin.Code)
	}
}

func newCredentialRevealServer(t *testing.T) (*Server, *time.Time) {
	t.Helper()
	hasher := auth.NewPasswordHasher(bytes.NewReader(bytes.Repeat([]byte{0x61}, 64)))
	passwordHash, err := hasher.Hash("panel-password")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	now := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	manager := newFakeNodeManager()
	sessionEntropy := append(
		bytes.Repeat([]byte{0x62}, 64),
		bytes.Repeat([]byte{0x63}, 64)...,
	)
	server := NewServer(Options{
		BasePath:     "/abcdefghijkl",
		PasswordHash: passwordHash,
		Hasher:       hasher,
		Sessions: auth.NewSessionManager(
			bytes.NewReader(sessionEntropy),
			clock,
		),
		Limiter:     auth.NewLoginLimiter(clock),
		Config:      manager.Snapshot(),
		NodeManager: manager,
		Clock:       clock,
	})
	return server, &now
}

func performCredentialRequest(t *testing.T, server *Server, path string, cookie *http.Cookie, csrfToken string, clientIP string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://panel.test/abcdefghijkl"+path, bytes.NewReader(body))
	request.RemoteAddr = clientIP + ":41234"
	request.Header.Set("Content-Type", "application/json")
	if csrfToken != "" {
		request.Header.Set("X-CSRF-Token", csrfToken)
	}
	request.AddCookie(cookie)
	server.Handler().ServeHTTP(response, request)
	return response
}
