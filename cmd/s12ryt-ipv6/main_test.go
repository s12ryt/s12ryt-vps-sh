package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vps-sh/internal/auth"
	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
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
