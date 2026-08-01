package panel

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vps-sh/internal/auth"
	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	hasher := auth.NewPasswordHasher(bytes.NewReader(bytes.Repeat([]byte{0x51}, 64)))
	passwordHash, err := hasher.Hash("panel-password")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	now := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	return NewServer(Options{
		BasePath:     "/abcdefghijkl",
		PasswordHash: passwordHash,
		Hasher:       hasher,
		Sessions: auth.NewSessionManager(
			bytes.NewReader(bytes.Repeat([]byte{0x52}, 1024)),
			func() time.Time { return now },
		),
		Limiter: auth.NewLoginLimiter(func() time.Time { return now }),
		Config:  domain.DefaultConfig(),
	})
}

func TestLoginPageWarnsAboutPublicHTTPAndHasNoExternalAssets(t *testing.T) {
	server := newTestServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://panel.test/abcdefghijkl", nil)
	request.RemoteAddr = "198.51.100.8:41234"
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	for _, expected := range []string{"登入", "公開 HTTP", "密碼可能被攔截"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("login page missing %q", expected)
		}
	}
	if regexp.MustCompile(`(?i)(src|href)=["']https?://`).MatchString(body) {
		t.Fatal("login page references an external CDN asset")
	}
}

func TestSuccessfulLoginSetsSessionCookieWithRequiredAttributes(t *testing.T) {
	server := newTestServer(t)
	response := login(t, server, "panel-password", "198.51.100.8")

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != SessionCookieName || cookie.Value == "" {
		t.Fatalf("cookie = %#v", cookie)
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie security = HttpOnly:%v SameSite:%v", cookie.HttpOnly, cookie.SameSite)
	}
	if cookie.Secure {
		t.Fatal("public HTTP session cookie unexpectedly has Secure=true")
	}
	if cookie.MaxAge != 0 || !cookie.Expires.IsZero() {
		t.Fatal("session cookie must expire when the browser closes")
	}
}

func TestLoginLocksSameIPAfterFiveFailures(t *testing.T) {
	server := newTestServer(t)
	for attempt := 1; attempt <= 5; attempt++ {
		response := login(t, server, "wrong-password", "198.51.100.8")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", attempt, response.Code)
		}
	}
	locked := login(t, server, "panel-password", "198.51.100.8")
	if locked.Code != http.StatusTooManyRequests {
		t.Fatalf("locked status = %d, want 429", locked.Code)
	}
	otherClient := login(t, server, "panel-password", "203.0.113.9")
	if otherClient.Code != http.StatusSeeOther {
		t.Fatalf("other client status = %d, want 303", otherClient.Code)
	}
}

func TestDashboardUsesRequiredNavigationOrderAndModalContract(t *testing.T) {
	server := newTestServer(t)
	loginResponse := login(t, server, "panel-password", "198.51.100.8")
	cookie := loginResponse.Result().Cookies()[0]

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://panel.test/abcdefghijkl", nil)
	request.RemoteAddr = "198.51.100.8:41234"
	request.AddCookie(cookie)
	server.Handler().ServeHTTP(response, request)
	body := response.Body.String()

	exitIndex := strings.Index(body, "出口模式")
	topologyIndex := strings.Index(body, "拓撲")
	protocolIndex := strings.Index(body, "協議")
	if exitIndex < 0 || topologyIndex <= exitIndex || protocolIndex <= topologyIndex {
		t.Fatalf("navigation order indexes = %d, %d, %d", exitIndex, topologyIndex, protocolIndex)
	}
	for _, expected := range []string{
		`data-modal-backdrop="static"`,
		`data-modal-close="button"`,
		`data-modal-close="escape"`,
		`name="csrf-token"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("dashboard missing %q", expected)
		}
	}
	if regexp.MustCompile(`(?i)(src|href)=["']https?://`).MatchString(body) {
		t.Fatal("dashboard references an external CDN asset")
	}
}

func TestStateChangingAPIRequiresSessionClientBindingAndCSRF(t *testing.T) {
	server := newTestServer(t)
	loginResponse := login(t, server, "panel-password", "198.51.100.8")
	cookie := loginResponse.Result().Cookies()[0]
	csrfToken := dashboardCSRF(t, server, cookie, "198.51.100.8")
	payload, err := json.Marshal(domain.DefaultConfig())
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	tests := []struct {
		name       string
		clientIP   string
		csrf       string
		wantStatus int
	}{
		{name: "missing CSRF", clientIP: "198.51.100.8", wantStatus: http.StatusForbidden},
		{name: "wrong CSRF", clientIP: "198.51.100.8", csrf: "wrong", wantStatus: http.StatusForbidden},
		{name: "different client", clientIP: "203.0.113.9", csrf: csrfToken, wantStatus: http.StatusUnauthorized},
		{name: "valid", clientIP: "198.51.100.8", csrf: csrfToken, wantStatus: http.StatusOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "http://panel.test/abcdefghijkl/api/config/validate", bytes.NewReader(payload))
			request.RemoteAddr = test.clientIP + ":41234"
			request.Header.Set("Content-Type", "application/json")
			if test.csrf != "" {
				request.Header.Set("X-CSRF-Token", test.csrf)
			}
			request.AddCookie(cookie)
			server.Handler().ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestConfigValidationIsStrictAndDoesNotPersistCandidate(t *testing.T) {
	store := &memoryConfigStore{}
	server := newTestServerWithStore(t, store)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")

	unknown := performConfigRequest(t, server, http.MethodPost, "/api/config/validate", cookie, csrfToken, "apply", []byte(`{"unknown":true}`))
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d, want 400", unknown.Code)
	}

	invalidConfig := domain.DefaultConfig()
	invalidConfig.Panel.Port = 0
	invalidPayload, _ := json.Marshal(invalidConfig)
	invalid := performConfigRequest(t, server, http.MethodPost, "/api/config/validate", cookie, csrfToken, "apply", invalidPayload)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid config status = %d, want 422", invalid.Code)
	}

	candidate := domain.DefaultConfig()
	candidate.Panel.Port = 35555
	candidatePayload, _ := json.Marshal(candidate)
	valid := performConfigRequest(t, server, http.MethodPost, "/api/config/validate", cookie, csrfToken, "apply", candidatePayload)
	if valid.Code != http.StatusOK || !strings.Contains(valid.Body.String(), `"changed":true`) {
		t.Fatalf("valid response = %d %s", valid.Code, valid.Body.String())
	}
	if len(store.saved) != 0 {
		t.Fatal("validation persisted candidate config")
	}
}

func TestConfigApplyRequiresConfirmationAndUpdatesCurrentConfig(t *testing.T) {
	store := &memoryConfigStore{}
	server := newTestServerWithStore(t, store)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")
	candidate := domain.DefaultConfig()
	candidate.Panel.Port = 35555
	payload, _ := json.Marshal(candidate)

	unconfirmed := performConfigRequest(t, server, http.MethodPost, "/api/config/apply", cookie, csrfToken, "", payload)
	if unconfirmed.Code != http.StatusConflict || len(store.saved) != 0 {
		t.Fatalf("unconfirmed apply = %d, saves = %d", unconfirmed.Code, len(store.saved))
	}

	applied := performConfigRequest(t, server, http.MethodPost, "/api/config/apply", cookie, csrfToken, "apply", payload)
	if applied.Code != http.StatusOK || len(store.saved) != 1 || store.saved[0].Panel.Port != 35555 {
		t.Fatalf("confirmed apply = %d, saves = %#v", applied.Code, store.saved)
	}

	current := performConfigRequest(t, server, http.MethodGet, "/api/config", cookie, "", "", nil)
	if current.Code != http.StatusOK || !strings.Contains(current.Body.String(), `"port":35555`) {
		t.Fatalf("current config = %d %s", current.Code, current.Body.String())
	}
}

func login(t *testing.T, server *Server, password string, clientIP string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"password": {password}}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://panel.test/abcdefghijkl/login", strings.NewReader(form.Encode()))
	request.RemoteAddr = clientIP + ":41234"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	server.Handler().ServeHTTP(response, request)
	return response
}

func dashboardCSRF(t *testing.T, server *Server, cookie *http.Cookie, clientIP string) string {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://panel.test/abcdefghijkl", nil)
	request.RemoteAddr = clientIP + ":41234"
	request.AddCookie(cookie)
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		body, _ := io.ReadAll(response.Result().Body)
		t.Fatalf("dashboard status = %d, body = %s", response.Code, body)
	}
	match := regexp.MustCompile(`name="csrf-token" content="([^"]+)"`).FindStringSubmatch(response.Body.String())
	if len(match) != 2 {
		t.Fatal("dashboard CSRF meta tag not found")
	}
	return match[1]
}

type memoryConfigStore struct {
	saved []domain.Config
}

func (store *memoryConfigStore) Save(config domain.Config) error {
	store.saved = append(store.saved, config)
	return nil
}

func newTestServerWithStore(t *testing.T, store ConfigStore) *Server {
	t.Helper()
	server := newTestServer(t)
	server.store = store
	return server
}

func authenticatedSession(t *testing.T, server *Server, clientIP string) (*http.Cookie, string) {
	t.Helper()
	loginResponse := login(t, server, "panel-password", clientIP)
	if loginResponse.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, want 303", loginResponse.Code)
	}
	cookie := loginResponse.Result().Cookies()[0]
	return cookie, dashboardCSRF(t, server, cookie, clientIP)
}

func performConfigRequest(t *testing.T, server *Server, method string, path string, cookie *http.Cookie, csrfToken string, confirmation string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, "http://panel.test/abcdefghijkl"+path, bytes.NewReader(body))
	request.RemoteAddr = "198.51.100.8:41234"
	request.Header.Set("Content-Type", "application/json")
	if csrfToken != "" {
		request.Header.Set("X-CSRF-Token", csrfToken)
	}
	if confirmation != "" {
		request.Header.Set("X-S12ryt-Confirm", confirmation)
	}
	request.AddCookie(cookie)
	server.Handler().ServeHTTP(response, request)
	return response
}
