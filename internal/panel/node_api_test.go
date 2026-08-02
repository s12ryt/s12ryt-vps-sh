package panel

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/nodes"
)

func TestNodeListReturnsMaskedSummaries(t *testing.T) {
	manager := newFakeNodeManager()
	server := newNodeAPIServer(t, manager)
	cookie, _ := authenticatedSession(t, server, "198.51.100.8")

	response := performNodeRequest(t, server, http.MethodGet, "/api/nodes", cookie, "", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	for _, expected := range []string{`"id":"existing-vless"`, `"protocol":"vless"`, `"port":24443`, `"enabled":true`, `"credential_configured":true`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("node list missing %q: %s", expected, body)
		}
	}
	for _, secret := range []string{`"credential":`, manager.config.Nodes[0].Credential.UUID} {
		if strings.Contains(body, secret) {
			t.Fatalf("node list leaked %q: %s", secret, body)
		}
	}
}

func TestNodeCreateRequiresCSRFConfirmationAndStrictInput(t *testing.T) {
	manager := newFakeNodeManager()
	server := newNodeAPIServer(t, manager)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")
	payload := []byte(`{"id":"new-vmess","protocol":"vmess","port":25555,"enabled":true}`)

	missingCSRF := performNodeRequest(t, server, http.MethodPost, "/api/nodes", cookie, "", "apply", payload)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", missingCSRF.Code)
	}
	unconfirmed := performNodeRequest(t, server, http.MethodPost, "/api/nodes", cookie, csrfToken, "", payload)
	if unconfirmed.Code != http.StatusConflict {
		t.Fatalf("unconfirmed status = %d, want 409", unconfirmed.Code)
	}
	withCredential := []byte(`{"id":"unsafe","protocol":"vless","enabled":true,"credential":{"uuid":"00000000-0000-4000-8000-000000000000"}}`)
	strict := performNodeRequest(t, server, http.MethodPost, "/api/nodes", cookie, csrfToken, "apply", withCredential)
	if strict.Code != http.StatusBadRequest {
		t.Fatalf("credential input status = %d, want 400", strict.Code)
	}
	if len(manager.createCalls) != 0 {
		t.Fatal("rejected create request reached node manager")
	}

	created := performNodeRequest(t, server, http.MethodPost, "/api/nodes", cookie, csrfToken, "apply", payload)
	if created.Code != http.StatusCreated || len(manager.createCalls) != 1 {
		t.Fatalf("created response = %d, calls = %#v", created.Code, manager.createCalls)
	}
	createCall := manager.createCalls[0]
	if createCall.ID != "new-vmess" || createCall.Protocol != domain.ProtocolVMess || createCall.Port != 25555 || !createCall.Enabled {
		t.Fatalf("create input = %#v", createCall)
	}
	if createCall.Deployment.NodeID != "" || len(createCall.Deployment.Listeners) != 0 {
		t.Fatalf("legacy node payload unexpectedly supplied deployment data: %#v", createCall.Deployment)
	}
	if strings.Contains(created.Body.String(), `"credential"`) || strings.Contains(created.Body.String(), manager.config.Nodes[1].Credential.UUID) {
		t.Fatalf("create response leaked credential: %s", created.Body.String())
	}
}

func TestNodeUpdateAndDeleteUseManagedOperations(t *testing.T) {
	manager := newFakeNodeManager()
	server := newNodeAPIServer(t, manager)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")

	updatePayload := []byte(`{"port":26666,"enabled":false}`)
	updated := performNodeRequest(t, server, http.MethodPatch, "/api/nodes/existing-vless", cookie, csrfToken, "apply", updatePayload)
	if updated.Code != http.StatusOK || len(manager.updateCalls) != 1 {
		t.Fatalf("updated response = %d, calls = %#v", updated.Code, manager.updateCalls)
	}
	if manager.updateCalls[0] != (nodes.UpdateInput{ID: "existing-vless", Port: 26666, Enabled: false}) {
		t.Fatalf("update input = %#v", manager.updateCalls[0])
	}

	unconfirmed := performNodeRequest(t, server, http.MethodDelete, "/api/nodes/existing-vless", cookie, csrfToken, "", nil)
	if unconfirmed.Code != http.StatusConflict || len(manager.deleteCalls) != 0 {
		t.Fatalf("unconfirmed delete = %d, calls = %#v", unconfirmed.Code, manager.deleteCalls)
	}
	deleted := performNodeRequest(t, server, http.MethodDelete, "/api/nodes/existing-vless", cookie, csrfToken, "apply", nil)
	if deleted.Code != http.StatusNoContent || len(manager.deleteCalls) != 1 || manager.deleteCalls[0] != "existing-vless" {
		t.Fatalf("deleted response = %d, calls = %#v", deleted.Code, manager.deleteCalls)
	}
}

func TestConfigApplyDelegatesToNodeManager(t *testing.T) {
	manager := newFakeNodeManager()
	server := newNodeAPIServer(t, manager)
	cookie, csrfToken := authenticatedSession(t, server, "198.51.100.8")
	current := performNodeRequest(t, server, http.MethodGet, "/api/config", cookie, "", "", nil)
	if current.Code != http.StatusOK {
		t.Fatalf("current config status = %d, want 200", current.Code)
	}
	for _, secret := range []string{`"credential":`, manager.config.Nodes[0].Credential.UUID} {
		if strings.Contains(current.Body.String(), secret) {
			t.Fatalf("config endpoint leaked %q: %s", secret, current.Body.String())
		}
	}
	var candidate map[string]any
	if err := json.Unmarshal(current.Body.Bytes(), &candidate); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	routing, ok := candidate["routing"].(map[string]any)
	if !ok {
		t.Fatalf("routing payload = %#v", candidate["routing"])
	}
	routing["mode"] = string(domain.RoutingModeIPv6Only)
	payload, _ := json.Marshal(candidate)

	response := performNodeRequest(t, server, http.MethodPost, "/api/config/apply", cookie, csrfToken, "apply", payload)
	if response.Code != http.StatusOK || len(manager.replaceCalls) != 1 {
		t.Fatalf("apply response = %d, replace calls = %#v", response.Code, manager.replaceCalls)
	}
	if manager.Snapshot().Routing.Mode != domain.RoutingModeIPv6Only {
		t.Fatal("node manager did not receive the replacement config")
	}
}

func newNodeAPIServer(t *testing.T, manager NodeManager) *Server {
	t.Helper()
	server := newTestServer(t)
	server.nodeManager = manager
	server.config = manager.Snapshot()
	return server
}

func performNodeRequest(t *testing.T, server *Server, method string, path string, cookie *http.Cookie, csrfToken string, confirmation string, body []byte) *httptest.ResponseRecorder {
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

type fakeNodeManager struct {
	config       domain.Config
	createCalls  []nodes.CreateInput
	updateCalls  []nodes.UpdateInput
	deleteCalls  []string
	replaceCalls []domain.Config
}

func newFakeNodeManager() *fakeNodeManager {
	config := domain.DefaultConfig()
	config.Nodes = []domain.Node{{
		ID: "existing-vless", Protocol: domain.ProtocolVLESS, Port: 24443, Enabled: true,
		Credential: domain.NodeCredential{UUID: "11111111-1111-4111-8111-111111111111"},
	}}
	return &fakeNodeManager{config: config}
}

func (manager *fakeNodeManager) Snapshot() domain.Config {
	config := manager.config
	config.Nodes = append([]domain.Node(nil), manager.config.Nodes...)
	return config
}

func (manager *fakeNodeManager) ReplaceConfig(config domain.Config) error {
	manager.replaceCalls = append(manager.replaceCalls, config)
	manager.config = config
	return nil
}

func (manager *fakeNodeManager) Create(input nodes.CreateInput) (domain.Node, error) {
	manager.createCalls = append(manager.createCalls, input)
	node := domain.Node{
		ID: input.ID, Protocol: input.Protocol, Port: input.Port, Enabled: input.Enabled,
		Credential: domain.NodeCredential{UUID: "22222222-2222-4222-8222-222222222222"},
	}
	manager.config.Nodes = append(manager.config.Nodes, node)
	return node, nil
}

func (manager *fakeNodeManager) Update(input nodes.UpdateInput) (domain.Node, error) {
	manager.updateCalls = append(manager.updateCalls, input)
	for index := range manager.config.Nodes {
		if manager.config.Nodes[index].ID == input.ID {
			manager.config.Nodes[index].Port = input.Port
			manager.config.Nodes[index].Enabled = input.Enabled
			return manager.config.Nodes[index], nil
		}
	}
	return domain.Node{}, nil
}

func (manager *fakeNodeManager) Delete(id string) error {
	manager.deleteCalls = append(manager.deleteCalls, id)
	return nil
}
