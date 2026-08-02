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

func TestHealthEndpointIsUnauthenticatedMinimalAndPathScoped(t *testing.T) {
	server := newTestServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://panel.test/abcdefghijkl/healthz", nil)
	request.RemoteAddr = "198.51.100.8:41234"
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", response.Code)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("health Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("health Content-Type = %q", response.Header().Get("Content-Type"))
	}
	if strings.TrimSpace(response.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("health body = %q", response.Body.String())
	}

	wrongPath := httptest.NewRecorder()
	server.Handler().ServeHTTP(wrongPath, httptest.NewRequest(http.MethodGet, "http://panel.test/healthz", nil))
	if wrongPath.Code != http.StatusNotFound {
		t.Fatalf("unscoped health status = %d, want 404", wrongPath.Code)
	}
}

func TestLoginPageUsesDarkNOCTheme(t *testing.T) {
	server := newTestServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://panel.test/abcdefghijkl", nil)
	request.RemoteAddr = "198.51.100.8:41234"
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	for _, expected := range []string{`data-ui-theme="noc"`, `color-scheme:dark`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("login page missing dark theme marker %q", expected)
		}
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

func TestResponsesSetSecurityAndNoStoreHeaders(t *testing.T) {
	server := newTestServer(t)

	for _, path := range []string{"/abcdefghijkl", "/abcdefghijkl/missing"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "http://panel.test"+path, nil)
		request.RemoteAddr = "198.51.100.8:41234"
		server.Handler().ServeHTTP(response, request)

		for name, expected := range map[string]string{
			"Cache-Control":           "no-store",
			"Content-Security-Policy": "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
			"Referrer-Policy":         "no-referrer",
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
		} {
			if actual := response.Header().Get(name); actual != expected {
				t.Fatalf("%s %s = %q, want %q", path, name, actual, expected)
			}
		}
	}
}

func TestLogoutRequiresCSRFRevokesSessionAndClearsCookie(t *testing.T) {
	server := newTestServer(t)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")

	missingCSRF := performConfigRequest(t, server, http.MethodPost, "/logout", cookie, "", "", nil)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", missingCSRF.Code)
	}

	response := performConfigRequest(t, server, http.MethodPost, "/logout", cookie, csrfToken, "", nil)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("logout status = %d, want 303", response.Code)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != SessionCookieName || cookies[0].MaxAge >= 0 {
		t.Fatalf("logout cookie = %#v", cookies)
	}

	dashboard := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://panel.test/abcdefghijkl", nil)
	request.RemoteAddr = "198.51.100.8:41234"
	request.AddCookie(cookie)
	server.Handler().ServeHTTP(dashboard, request)
	if !strings.Contains(dashboard.Body.String(), "登入 IPv6 管理面板") {
		t.Fatal("revoked session still reached the dashboard")
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
		`action="/abcdefghijkl/logout"`,
		`name="csrf_token"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("dashboard missing %q", expected)
		}
	}
	if regexp.MustCompile(`(?i)(src|href)=["']https?://`).MatchString(body) {
		t.Fatal("dashboard references an external CDN asset")
	}
}

func TestDashboardProvidesDarkFiveWorkspaceNavigation(t *testing.T) {
	server := newTestServer(t)
	cookie, _ := authenticatedSession(t, server, "198.51.100.8")

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://panel.test/abcdefghijkl", nil)
	request.RemoteAddr = "198.51.100.8:41234"
	request.AddCookie(cookie)
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200", response.Code)
	}
	body := response.Body.String()

	for _, expected := range []string{
		`data-ui-theme="noc"`,
		`color-scheme:dark`,
		`class="workspace-nav"`,
		`role="tablist"`,
		`role="tab" id="workspace-tab-strategy"`,
		`role="tab" id="workspace-tab-nodes"`,
		`role="tab" id="workspace-tab-remotes"`,
		`role="tab" id="workspace-tab-network"`,
		`role="tab" id="workspace-tab-shares"`,
		`data-workspace="strategy"`,
		`data-workspace="nodes"`,
		`data-workspace="remotes"`,
		`data-workspace="network"`,
		`data-workspace="shares"`,
		`#strategy`,
		`#nodes`,
		`#remotes`,
		`#network`,
		`#shares`,
		`addEventListener('hashchange'`,
		`history.replaceState`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("dashboard missing workspace marker %q", expected)
		}
	}
	if strings.Count(body, `role="tabpanel"`) != 5 {
		t.Fatalf("workspace panel count = %d, want 5", strings.Count(body, `role="tabpanel"`))
	}
}

func TestDashboardProvidesModeTopologyAndProtocolConfiguration(t *testing.T) {
	server := newTestServer(t)
	cookie, _ := authenticatedSession(t, server, "198.51.100.8")

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://panel.test/abcdefghijkl", nil)
	request.RemoteAddr = "198.51.100.8:41234"
	request.AddCookie(cookie)
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200", response.Code)
	}
	body := response.Body.String()

	for _, expected := range []string{
		`data-section-target="routing"`,
		`data-section-target="topology"`,
		`data-section-target="protocols"`,
		`data-config-endpoint="/abcdefghijkl/api/config"`,
		`data-validate-endpoint="/abcdefghijkl/api/config/validate"`,
		`data-apply-endpoint="/abcdefghijkl/api/config/apply"`,
		`name="routing_mode" value="client-ipv4"`,
		`name="routing_mode" value="vps-ipv4" checked`,
		`name="routing_mode" value="ipv6-only"`,
		`name="topology" value="multi-ipv6-multi-node" checked`,
		`name="topology" value="single-ipv6-single-node"`,
		`name="topology" value="multi-ipv6-rotating-node"`,
		`name="topology" value="multi-ipv6-rotating-nodes"`,
		`data-protocol="vless"`,
		`data-protocol="vmess"`,
		`data-protocol="hysteria2"`,
		`data-protocol="tuic"`,
		`data-protocol="socks5"`,
		`data-protocol="anytls"`,
		`data-protocol="shadowsocks"`,
		`X-CSRF-Token`,
		`X-S12ryt-Confirm`,
		`data-config-diff`,
		`data-config-error`,
		`data-nodes-endpoint="/abcdefghijkl/api/nodes"`,
		`data-credential-endpoint-template="/abcdefghijkl/api/nodes/{id}/credential"`,
		`data-node-table`,
		`data-node-form`,
		`name="node_id"`,
		`name="node_protocol"`,
		`name="node_port"`,
		`name="node_enabled"`,
		`name="node_listener_ipv4"`,
		`name="node_listener_ipv6"`,
		`name="node_tls_enabled"`,
		`name="node_tls_mode"`,
		`value="certificate"`,
		`value="acme"`,
		`name="node_server_name"`,
		`name="node_certificate_path"`,
		`name="node_key_path"`,
		`name="node_acme_domains"`,
		`name="node_acme_default_server_name"`,
		`name="node_acme_email"`,
		`/opt/s12ryt-ipv6/tls/acme`,
		`provider:'letsencrypt'`,
		`acme:`,
		`name="node_transport"`,
		`value="tcp"`,
		`value="websocket"`,
		`value="grpc"`,
		`name="node_transport_path"`,
		`name="node_grpc_service_name"`,
		`deployment:`,
		`listeners:`,
		`transport:`,
		`data-node-create`,
		`data-node-edit`,
		`data-node-delete`,
		`data-credential-form`,
		`name="management_password"`,
		`autocomplete="current-password"`,
		`data-credential-reveal`,
		`data-credential-value hidden`,
		`data-credential-expiry`,
		`expires_in_seconds`,
		`data-remotes-endpoint="/abcdefghijkl/api/remotes"`,
		`data-ipv4-fallback-endpoint="/abcdefghijkl/api/ipv4-fallback"`,
		`data-remote-workspace`,
		`data-remote-table`,
		`data-remote-import`,
		`data-remote-form`,
		`name="remote_payload"`,
		`name="allow_ipv4_proxy"`,
		`name="remote_enabled"`,
		`data-remote-toggle`,
		`data-remote-delete`,
		`data-fallback-form`,
		`name="ipv4_fallback_tags"`,
		`allow_ipv4_proxy:`,
		`data-network-addresses-endpoint="/abcdefghijkl/api/network/addresses"`,
		`data-network-apply-endpoint="/abcdefghijkl/api/network/apply"`,
		`data-network-workspace`,
		`data-network-address-table`,
		`data-network-refresh`,
		`data-network-form`,
		`name="network_interface"`,
		`name="network_prefix"`,
		`name="network_count"`,
		`name="firewall_backend"`,
		`name="allowed_cidrs"`,
		`name="node_ports"`,
		`data-network-apply`,
		`data-network-result`,
		`address_count`,
		`firewall_backend`,
		`data-share-reveal-endpoint="/abcdefghijkl/api/shares/reveal"`,
		`data-share-qr-endpoint-template="/abcdefghijkl/api/shares/{id}/qr"`,
		`data-share-workspace`,
		`data-share-open`,
		`data-share-form`,
		`name="share_management_password"`,
		`autocomplete="current-password"`,
		`data-share-nodes`,
		`data-share-uri`,
		`data-share-client-json`,
		`data-share-full-client-json`,
		`data-share-full-client-base64`,
		`data-share-warning`,
		`data-share-qr`,
		`data-share-subscription`,
		`data-share-expiry`,
		`data-share-copy`,
		`expires_in_seconds`,
		`full_client_json`,
		`full_client_base64`,
		`qr_url`,
		`clearShareSecrets`,
		`data-operation-notice`,
		`role="status" aria-live="polite"`,
		`runOperation`,
		`aria-busy`,
		`inFlightOperations`,
		`@media(max-width:760px){.grid{grid-template-columns:minmax(0,1fr)}.grid>*{grid-column:1!important;min-width:0}}`,
		`X-CSRF-Token`,
		`X-S12ryt-Confirm`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("dashboard missing configuration marker %q", expected)
		}
	}
	if strings.Count(body, `data-modal-backdrop="static"`) != 8 {
		t.Fatalf("static modal count = %d, want 8", strings.Count(body, `data-modal-backdrop="static"`))
	}
	if strings.Contains(body, "innerHTML") {
		t.Fatal("dashboard remote UI must not render API data through innerHTML")
	}
	for _, secret := range []string{"vless://", "$pbkdf2-sha256$"} {
		if strings.Contains(body, secret) {
			t.Fatalf("dashboard rendered protected share material %q before reveal", secret)
		}
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
