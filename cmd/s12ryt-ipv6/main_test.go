package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vps-sh/internal/auth"
	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	projectnetwork "github.com/s12ryt/s12ryt-vps-sh/internal/network"
	"github.com/s12ryt/s12ryt-vps-sh/internal/store"
)

func TestLoadApplicationBuildsPanelFromProtectedFiles(t *testing.T) {
	paths := writeRuntimeFiles(t, 0o600)
	application, err := loadApplication(runtimeOptions{
		ConfigPath:       paths.config,
		PasswordHashPath: paths.passwordHash,
		Entropy:          bytes.NewReader(bytes.Repeat([]byte{0x31}, 128)),
		Clock:            func() time.Time { return time.Unix(1_800_000_000, 0) },
	})
	if err != nil {
		t.Fatalf("loadApplication returned error: %v", err)
	}
	if application.address != "[::]:34456" {
		t.Fatalf("address = %q, want [::]:34456", application.address)
	}

	request := httptest.NewRequest(http.MethodGet, "/configureme1", nil)
	request.RemoteAddr = "198.51.100.20:43210"
	response := httptest.NewRecorder()
	application.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("panel status = %d, want 200", response.Code)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte("登入 IPv6 管理面板")) {
		t.Fatalf("panel body missing login page: %s", response.Body.String())
	}
}

func TestLoadApplicationRejectsUnprotectedPasswordHash(t *testing.T) {
	paths := writeRuntimeFiles(t, 0o644)
	_, err := loadApplication(runtimeOptions{
		ConfigPath:       paths.config,
		PasswordHashPath: paths.passwordHash,
	})
	if err == nil {
		t.Fatal("loadApplication accepted a group/world-readable password hash")
	}
}

func TestLoadApplicationWiresManagedNodeCreationToPortChecker(t *testing.T) {
	paths := writeRuntimeFiles(t, 0o600)
	checker := &runtimePortChecker{}
	application, err := loadApplication(runtimeOptions{
		ConfigPath:             paths.config,
		PasswordHashPath:       paths.passwordHash,
		Entropy:                bytes.NewReader(bytes.Repeat([]byte{0x31}, 512)),
		Clock:                  func() time.Time { return time.Unix(1_800_000_000, 0) },
		PortChecker:            checker,
		PortAllocationAttempts: 4,
	})
	if err != nil {
		t.Fatalf("loadApplication returned error: %v", err)
	}

	cookie := runtimeLogin(t, application.handler)
	dashboard := httptest.NewRecorder()
	dashboardRequest := httptest.NewRequest(http.MethodGet, "/configureme1", nil)
	dashboardRequest.RemoteAddr = "198.51.100.20:43210"
	dashboardRequest.AddCookie(cookie)
	application.handler.ServeHTTP(dashboard, dashboardRequest)
	csrfMatch := regexp.MustCompile(`name="csrf-token" content="([^"]+)"`).FindStringSubmatch(dashboard.Body.String())
	if dashboard.Code != http.StatusOK || len(csrfMatch) != 2 {
		t.Fatalf("dashboard response = %d, csrf match = %#v", dashboard.Code, csrfMatch)
	}

	payload, _ := json.Marshal(map[string]any{
		"id": "runtime-vless", "protocol": "vless", "port": 0, "enabled": true,
	})
	created := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/configureme1/api/nodes", bytes.NewReader(payload))
	createRequest.RemoteAddr = "198.51.100.20:43210"
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("X-CSRF-Token", csrfMatch[1])
	createRequest.Header.Set("X-S12ryt-Confirm", "apply")
	createRequest.AddCookie(cookie)
	application.handler.ServeHTTP(created, createRequest)
	if created.Code != http.StatusCreated {
		t.Fatalf("node create status = %d, body = %s", created.Code, created.Body.String())
	}
	if len(checker.checks) != 2 || checker.checks[0].network != "tcp" || checker.checks[1].network != "udp" || checker.checks[0].port != checker.checks[1].port {
		t.Fatalf("port checks = %#v, want matching TCP then UDP checks", checker.checks)
	}
	if checker.checks[0].port < 20000 || checker.checks[0].port > 49999 {
		t.Fatalf("allocated port = %d, want 20000-49999", checker.checks[0].port)
	}
}

func TestRunApplicationListensOnDualStackAndShutsDownGracefully(t *testing.T) {
	paths := writeRuntimeFiles(t, 0o600)
	listener := &stubListener{}
	server := newStubHTTPServer()
	var network, address string
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- runApplication(ctx, runtimeOptions{
			ConfigPath:       paths.config,
			PasswordHashPath: paths.passwordHash,
			Entropy:          bytes.NewReader(bytes.Repeat([]byte{0x42}, 128)),
			Clock:            func() time.Time { return time.Unix(1_800_000_000, 0) },
			Listen: func(gotNetwork string, gotAddress string) (net.Listener, error) {
				network, address = gotNetwork, gotAddress
				return listener, nil
			},
			NewHTTPServer: func(_ string, _ http.Handler) managedHTTPServer {
				return server
			},
		})
	}()

	select {
	case <-server.started:
	case <-time.After(time.Second):
		t.Fatal("HTTP server did not start")
	}
	if network != "tcp" || address != "[::]:34456" {
		t.Fatalf("listen = %q %q, want tcp [::]:34456", network, address)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runApplication returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runApplication did not stop after cancellation")
	}
	if !server.wasShutdown() {
		t.Fatal("HTTP server was not shut down gracefully")
	}
}

type runtimePaths struct {
	config       string
	passwordHash string
}

func writeRuntimeFiles(t *testing.T, passwordMode os.FileMode) runtimePaths {
	t.Helper()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	configStore := store.NewConfigStore(configPath)
	if err := configStore.Save(domain.DefaultConfig()); err != nil {
		t.Fatalf("save configuration: %v", err)
	}
	hasher := auth.NewPasswordHasher(bytes.NewReader(bytes.Repeat([]byte{0x19}, 32)))
	passwordHash, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	passwordPath := filepath.Join(directory, "password.hash")
	if err := os.WriteFile(passwordPath, []byte(passwordHash+"\n"), passwordMode); err != nil {
		t.Fatalf("write password hash: %v", err)
	}
	return runtimePaths{config: configPath, passwordHash: passwordPath}
}

func runtimeLogin(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	login := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/configureme1/login", bytes.NewBufferString(url.Values{
		"password": {"correct horse battery staple"},
	}.Encode()))
	request.RemoteAddr = "198.51.100.20:43210"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(login, request)
	if login.Code != http.StatusSeeOther || len(login.Result().Cookies()) != 1 {
		t.Fatalf("login response = %d, cookies = %#v", login.Code, login.Result().Cookies())
	}
	return login.Result().Cookies()[0]
}

type runtimePortCheck struct {
	network string
	port    int
}

type runtimePortChecker struct {
	checks []runtimePortCheck
}

func (checker *runtimePortChecker) Available(network string, port int) (bool, error) {
	checker.checks = append(checker.checks, runtimePortCheck{network: network, port: port})
	return true, nil
}

var _ projectnetwork.PortAvailabilityChecker = (*runtimePortChecker)(nil)

type stubListener struct{}

func (listener *stubListener) Accept() (net.Conn, error) { return nil, errors.New("not used") }
func (listener *stubListener) Close() error              { return nil }
func (listener *stubListener) Addr() net.Addr            { return stubAddr("[::]:34456") }

type stubAddr string

func (address stubAddr) Network() string { return "tcp" }
func (address stubAddr) String() string  { return string(address) }

type stubHTTPServer struct {
	started  chan struct{}
	stopped  chan struct{}
	mutex    sync.Mutex
	shutdown bool
}

func newStubHTTPServer() *stubHTTPServer {
	return &stubHTTPServer{started: make(chan struct{}), stopped: make(chan struct{})}
}

func (server *stubHTTPServer) Serve(net.Listener) error {
	close(server.started)
	<-server.stopped
	return http.ErrServerClosed
}

func (server *stubHTTPServer) Shutdown(context.Context) error {
	server.mutex.Lock()
	if !server.shutdown {
		server.shutdown = true
		close(server.stopped)
	}
	server.mutex.Unlock()
	return nil
}

func (server *stubHTTPServer) wasShutdown() bool {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return server.shutdown
}
