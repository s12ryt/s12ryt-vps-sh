package panel

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/manifest"
	projectnetwork "github.com/s12ryt/s12ryt-vps-sh/internal/network"
	"github.com/s12ryt/s12ryt-vps-sh/internal/networksetup"
)

func TestNetworkAPIListsConfiguredGlobalIPv6Addresses(t *testing.T) {
	manager := newFakeNetworkManager()
	server := newNetworkAPIServer(t, manager)
	cookie, _ := authenticatedSession(t, server, "198.51.100.8")

	response := performNetworkRequest(t, server, http.MethodGet, "/api/network/addresses", cookie, "", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	for _, expected := range []string{
		`"interface":"eth0"`,
		`"address":"2001:0db8:0000:0000:0000:0000:0000:0001"`,
		`"prefix":"2001:db8::1/64"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("network address list missing %q: %s", expected, body)
		}
	}
}

func TestNetworkApplyRequiresSessionCSRFConfirmationAndStrictInput(t *testing.T) {
	manager := newFakeNetworkManager()
	server := newNetworkAPIServer(t, manager)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")
	payload := []byte(`{"interface":"eth0","prefix":"2001:db8:100::/64","count":16,"firewall_backend":"nftables","panel_port":34456,"allowed_cidrs":["0.0.0.0/0","::/0"],"node_ports":[{"port":24443,"protocol":"tcp"}]}`)

	missingCSRF := performNetworkRequest(t, server, http.MethodPost, "/api/network/apply", cookie, "", "apply", payload)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", missingCSRF.Code)
	}
	unconfirmed := performNetworkRequest(t, server, http.MethodPost, "/api/network/apply", cookie, csrfToken, "", payload)
	if unconfirmed.Code != http.StatusConflict {
		t.Fatalf("unconfirmed status = %d, want 409", unconfirmed.Code)
	}
	unknown := performNetworkRequest(t, server, http.MethodPost, "/api/network/apply", cookie, csrfToken, "apply", []byte(`{"interface":"eth0","unknown":true}`))
	if unknown.Code != http.StatusBadRequest || len(manager.applyCalls) != 0 {
		t.Fatalf("unknown response = %d, calls = %#v", unknown.Code, manager.applyCalls)
	}

	applied := performNetworkRequest(t, server, http.MethodPost, "/api/network/apply", cookie, csrfToken, "apply", payload)
	if applied.Code != http.StatusOK || len(manager.applyCalls) != 1 {
		t.Fatalf("applied response = %d, calls = %#v", applied.Code, manager.applyCalls)
	}
	want := networksetup.Request{
		Interface: "eth0", Prefix: "2001:db8:100::/64", Count: 16,
		FirewallBackend: "nftables", PanelPort: 34456,
		AllowedCIDRs: []string{"0.0.0.0/0", "::/0"},
		NodePorts:    []manifest.PortManifest{{Port: 24443, Protocol: "tcp"}},
	}
	if !reflect.DeepEqual(manager.applyCalls[0], want) {
		t.Fatalf("apply call = %#v, want %#v", manager.applyCalls[0], want)
	}
	for _, expected := range []string{`"interface":"eth0"`, `"address_count":2`, `"firewall_backend":"nftables"`} {
		if !strings.Contains(applied.Body.String(), expected) {
			t.Fatalf("apply response missing %q: %s", expected, applied.Body.String())
		}
	}
}

func TestNetworkAPIRejectsMissingManagerAndWrongClientBinding(t *testing.T) {
	server := newTestServer(t)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")
	missing := performNetworkRequest(t, server, http.MethodGet, "/api/network/addresses", cookie, "", "", nil)
	if missing.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing manager status = %d, want 503", missing.Code)
	}

	server.networkManager = newFakeNetworkManager()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://panel.test/abcdefghijkl/api/network/apply", bytes.NewReader([]byte(`{"interface":"eth0"}`)))
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

func newNetworkAPIServer(t *testing.T, manager NetworkManager) *Server {
	t.Helper()
	server := newTestServer(t)
	server.networkManager = manager
	return server
}

func performNetworkRequest(t *testing.T, server *Server, method string, path string, cookie *http.Cookie, csrfToken string, confirmation string, body []byte) *httptest.ResponseRecorder {
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

type fakeNetworkManager struct {
	addresses  []projectnetwork.InterfaceAddress
	applyCalls []networksetup.Request
}

func newFakeNetworkManager() *fakeNetworkManager {
	return &fakeNetworkManager{addresses: []projectnetwork.InterfaceAddress{{
		Interface: "eth0",
		Prefix:    netip.MustParsePrefix("2001:db8::1/64"),
	}}}
}

func (manager *fakeNetworkManager) GlobalIPv6Addresses(context.Context) ([]projectnetwork.InterfaceAddress, error) {
	return append([]projectnetwork.InterfaceAddress(nil), manager.addresses...), nil
}

func (manager *fakeNetworkManager) Apply(_ context.Context, request networksetup.Request) (manifest.Manifest, error) {
	request.AllowedCIDRs = append([]string(nil), request.AllowedCIDRs...)
	request.NodePorts = append([]manifest.PortManifest(nil), request.NodePorts...)
	manager.applyCalls = append(manager.applyCalls, request)
	return manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Interface:     request.Interface,
		Prefix:        request.Prefix,
		Gateway:       "fe80::1",
		Addresses:     []string{"2001:db8:100::1", "2001:db8:100::2"},
		Firewall: manifest.FirewallManifest{
			Backend:      request.FirewallBackend,
			PanelPort:    request.PanelPort,
			AllowedCIDRs: append([]string(nil), request.AllowedCIDRs...),
			NodePorts:    append([]manifest.PortManifest(nil), request.NodePorts...),
		},
	}, nil
}
