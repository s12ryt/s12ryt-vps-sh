package panel

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/nodes"
)

func TestRemoteOutboundListReturnsMaskedRuntimeSummaries(t *testing.T) {
	manager := newFakeRemoteManager()
	server := newRemoteAPIServer(t, manager)
	cookie, _ := authenticatedSession(t, server, "198.51.100.8")

	response := performRemoteRequest(t, server, http.MethodGet, "/api/remotes", cookie, "", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	for _, expected := range []string{`"tag":"remote-main"`, `"type":"vless"`, `"server":"proxy.example.com"`, `"port":443`, `"enabled":true`, `"ipv4_fallback_position":0`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("remote list missing %q: %s", expected, body)
		}
	}
	for _, secret := range []string{"550e8400", "proxy-secret", `"config"`, `"credential"`} {
		if strings.Contains(body, secret) {
			t.Fatalf("remote list leaked %q: %s", secret, body)
		}
	}
}

func TestRemoteImportRequiresCSRFConfirmationAndStrictInput(t *testing.T) {
	manager := newFakeRemoteManager()
	server := newRemoteAPIServer(t, manager)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")
	payload := []byte(`{"payload":"vless://550e8400-e29b-41d4-a716-446655440000@new.example.com:443","allow_ipv4_proxy":false,"enabled":true}`)

	missingCSRF := performRemoteRequest(t, server, http.MethodPost, "/api/remotes", cookie, "", "apply", payload)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", missingCSRF.Code)
	}
	unconfirmed := performRemoteRequest(t, server, http.MethodPost, "/api/remotes", cookie, csrfToken, "", payload)
	if unconfirmed.Code != http.StatusConflict {
		t.Fatalf("unconfirmed status = %d, want 409", unconfirmed.Code)
	}
	unknown := performRemoteRequest(t, server, http.MethodPost, "/api/remotes", cookie, csrfToken, "apply", []byte(`{"payload":"vless://example","unknown":true}`))
	if unknown.Code != http.StatusBadRequest || len(manager.importCalls) != 0 {
		t.Fatalf("unknown field response = %d, calls = %#v", unknown.Code, manager.importCalls)
	}

	created := performRemoteRequest(t, server, http.MethodPost, "/api/remotes", cookie, csrfToken, "apply", payload)
	if created.Code != http.StatusCreated || len(manager.importCalls) != 1 {
		t.Fatalf("created response = %d, calls = %#v", created.Code, manager.importCalls)
	}
	want := nodes.ImportRemoteInput{
		Payload: []byte("vless://550e8400-e29b-41d4-a716-446655440000@new.example.com:443"),
		Enabled: true,
	}
	if !reflect.DeepEqual(manager.importCalls[0], want) {
		t.Fatalf("import call = %#v, want %#v", manager.importCalls[0], want)
	}
	if strings.Contains(created.Body.String(), "550e8400") || strings.Contains(created.Body.String(), "proxy-secret") {
		t.Fatalf("import response leaked input secrets: %s", created.Body.String())
	}
}

func TestRemoteMutationAndIPv4FallbackUseManagedOperations(t *testing.T) {
	manager := newFakeRemoteManager()
	server := newRemoteAPIServer(t, manager)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")

	updated := performRemoteRequest(t, server, http.MethodPatch, "/api/remotes/remote-main", cookie, csrfToken, "apply", []byte(`{"enabled":false}`))
	if updated.Code != http.StatusOK || !reflect.DeepEqual(manager.updateCalls, []remoteUpdateCall{{tag: "remote-main", enabled: false}}) {
		t.Fatalf("update response = %d, calls = %#v", updated.Code, manager.updateCalls)
	}

	unconfirmedDelete := performRemoteRequest(t, server, http.MethodDelete, "/api/remotes/remote-main", cookie, csrfToken, "", nil)
	if unconfirmedDelete.Code != http.StatusConflict || len(manager.deleteCalls) != 0 {
		t.Fatalf("unconfirmed delete = %d, calls = %#v", unconfirmedDelete.Code, manager.deleteCalls)
	}
	deleted := performRemoteRequest(t, server, http.MethodDelete, "/api/remotes/remote-main", cookie, csrfToken, "apply", nil)
	if deleted.Code != http.StatusNoContent || !reflect.DeepEqual(manager.deleteCalls, []string{"remote-main"}) {
		t.Fatalf("delete response = %d, calls = %#v", deleted.Code, manager.deleteCalls)
	}

	fallback := []byte(`{"tags":["remote-socks","direct-v4"]}`)
	fallbackResponse := performRemoteRequest(t, server, http.MethodPut, "/api/ipv4-fallback", cookie, csrfToken, "apply", fallback)
	if fallbackResponse.Code != http.StatusNoContent || !reflect.DeepEqual(manager.fallbackCalls, [][]string{{"remote-socks", "direct-v4"}}) {
		t.Fatalf("fallback response = %d, calls = %#v", fallbackResponse.Code, manager.fallbackCalls)
	}
}

func TestRemoteAPIRejectsMissingManagerAndWrongClientBinding(t *testing.T) {
	server := newTestServer(t)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")
	missing := performRemoteRequest(t, server, http.MethodGet, "/api/remotes", cookie, "", "", nil)
	if missing.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing manager status = %d, want 503", missing.Code)
	}

	server.remoteManager = newFakeRemoteManager()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://panel.test/abcdefghijkl/api/remotes", bytes.NewReader([]byte(`{"payload":"vless://example"}`)))
	request.RemoteAddr = "198.51.100.99:41234"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrfToken)
	request.Header.Set("X-S12ryt-Confirm", "apply")
	request.AddCookie(cookie)
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong client status = %d, want 401", response.Code)
	}
}

func newRemoteAPIServer(t *testing.T, manager RemoteManager) *Server {
	t.Helper()
	server := newTestServer(t)
	server.remoteManager = manager
	return server
}

func performRemoteRequest(t *testing.T, server *Server, method string, path string, cookie *http.Cookie, csrfToken string, confirmation string, body []byte) *httptest.ResponseRecorder {
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

type remoteUpdateCall struct {
	tag     string
	enabled bool
}

type fakeRemoteManager struct {
	summaries     []nodes.RemoteOutboundSummary
	importCalls   []nodes.ImportRemoteInput
	updateCalls   []remoteUpdateCall
	deleteCalls   []string
	fallbackCalls [][]string
}

func newFakeRemoteManager() *fakeRemoteManager {
	return &fakeRemoteManager{summaries: []nodes.RemoteOutboundSummary{{
		Tag: "remote-main", Type: "vless", Server: "proxy.example.com", Port: 443, Enabled: true,
	}}}
}

func (manager *fakeRemoteManager) RemoteOutbounds() []nodes.RemoteOutboundSummary {
	return append([]nodes.RemoteOutboundSummary(nil), manager.summaries...)
}

func (manager *fakeRemoteManager) ImportRemoteOutbounds(input nodes.ImportRemoteInput) ([]nodes.RemoteOutboundSummary, error) {
	input.Payload = append([]byte(nil), input.Payload...)
	manager.importCalls = append(manager.importCalls, input)
	created := nodes.RemoteOutboundSummary{Tag: "remote-2", Type: "vless", Server: "new.example.com", Port: 443, Enabled: input.Enabled}
	manager.summaries = append(manager.summaries, created)
	return []nodes.RemoteOutboundSummary{created}, nil
}

func (manager *fakeRemoteManager) UpdateRemoteOutbound(tag string, enabled bool) (nodes.RemoteOutboundSummary, error) {
	manager.updateCalls = append(manager.updateCalls, remoteUpdateCall{tag: tag, enabled: enabled})
	return nodes.RemoteOutboundSummary{Tag: tag, Type: "vless", Server: "proxy.example.com", Port: 443, Enabled: enabled}, nil
}

func (manager *fakeRemoteManager) DeleteRemoteOutbound(tag string) error {
	manager.deleteCalls = append(manager.deleteCalls, tag)
	return nil
}

func (manager *fakeRemoteManager) SetIPv4Fallback(tags []string) error {
	manager.fallbackCalls = append(manager.fallbackCalls, append([]string(nil), tags...))
	return nil
}
