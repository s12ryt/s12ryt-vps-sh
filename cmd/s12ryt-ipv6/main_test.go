package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vps-sh/internal/auth"
	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/endpoint"
	"github.com/s12ryt/s12ryt-vps-sh/internal/manifest"
	projectnetwork "github.com/s12ryt/s12ryt-vps-sh/internal/network"
	"github.com/s12ryt/s12ryt-vps-sh/internal/networksetup"
	"github.com/s12ryt/s12ryt-vps-sh/internal/panel"
	"github.com/s12ryt/s12ryt-vps-sh/internal/runtimeconfig"
	"github.com/s12ryt/s12ryt-vps-sh/internal/store"
)

func TestLoadApplicationBuildsPanelFromProtectedFiles(t *testing.T) {
	paths := writeRuntimeFiles(t, 0o600)
	application, err := loadApplication(runtimeOptions{
		ConfigPath:       paths.config,
		PasswordHashPath: paths.passwordHash,
		RuntimeStatePath: paths.runtimeState,
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

func TestInitializeProjectCreatesProtectedBootstrapState(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	result, err := initializeProject(initializationOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x35}, 512)),
	})
	if err != nil {
		t.Fatalf("initializeProject() error = %v", err)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9]{24}$`).MatchString(result.Password) {
		t.Fatalf("password = %q, want 24 alphanumeric characters", result.Password)
	}
	if !regexp.MustCompile(`^/[A-Za-z0-9]{12}$`).MatchString(result.WebPath) {
		t.Fatalf("web path = %q, want slash and 12 alphanumeric characters", result.WebPath)
	}

	configPath := filepath.Join(projectRoot, "config", "config.json")
	config, err := store.NewConfigStore(configPath).Load()
	if err != nil {
		t.Fatalf("load initialized config: %v", err)
	}
	if config.Panel.Path != result.WebPath || config.Panel.Port != 34456 {
		t.Fatalf("initialized panel = %#v, result = %#v", config.Panel, result)
	}

	passwordPath := filepath.Join(projectRoot, "secrets", "password.hash")
	info, err := os.Stat(passwordPath)
	if err != nil {
		t.Fatalf("stat password hash: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("password hash mode = %04o, want 0600", info.Mode().Perm())
	}
	encoded, err := readProtectedPasswordHash(passwordPath)
	if err != nil {
		t.Fatalf("read password hash: %v", err)
	}
	verified, err := auth.NewPasswordHasher(nil).Verify(encoded, result.Password)
	if err != nil || !verified {
		t.Fatalf("Verify(initial password) = %t, %v", verified, err)
	}
	plainPasswordPath := filepath.Join(projectRoot, "secrets", "management.password")
	plainPasswordInfo, err := os.Stat(plainPasswordPath)
	if err != nil {
		t.Fatalf("stat plaintext management password: %v", err)
	}
	if plainPasswordInfo.Mode().Perm() != 0o600 {
		t.Fatalf("plaintext password mode = %04o, want 0600", plainPasswordInfo.Mode().Perm())
	}
	plainPassword, err := os.ReadFile(plainPasswordPath)
	if err != nil {
		t.Fatalf("read plaintext management password: %v", err)
	}
	if string(plainPassword) != result.Password+"\n" {
		t.Fatalf("stored plaintext password = %q, want generated password", plainPassword)
	}
	runtimeStatePath := filepath.Join(projectRoot, "state", "runtime.json")
	runtimeStateStore, err := runtimeconfig.NewDeploymentStateStore(runtimeStatePath)
	if err != nil {
		t.Fatalf("NewDeploymentStateStore() error = %v", err)
	}
	runtimeState, err := runtimeStateStore.Load()
	if err != nil {
		t.Fatalf("load initialized runtime state: %v", err)
	}
	if runtimeState.SchemaVersion != runtimeconfig.DeploymentStateSchemaVersion || len(runtimeState.Nodes) != 0 || len(runtimeState.IPv6Outbounds) != 0 {
		t.Fatalf("initialized runtime state = %#v, want empty schema state", runtimeState)
	}
	if info, err := os.Stat(runtimeStatePath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("runtime state mode = %v, %v, want 0600", info, err)
	}
	runtimeInput, err := runtimeState.Resolve(config)
	if err != nil {
		t.Fatalf("Resolve(initial runtime state) error = %v", err)
	}
	wantRuntimeConfig, err := runtimeconfig.CompileServerConfig(runtimeInput)
	if err != nil {
		t.Fatalf("CompileServerConfig(initial runtime state) error = %v", err)
	}
	runtimeConfigPath := filepath.Join(projectRoot, "config", "sing-box.json")
	runtimeConfig, err := os.ReadFile(runtimeConfigPath)
	if err != nil {
		t.Fatalf("read initialized sing-box config: %v", err)
	}
	if !bytes.Equal(runtimeConfig, wantRuntimeConfig) {
		t.Fatalf("initialized sing-box config = %s, want %s", runtimeConfig, wantRuntimeConfig)
	}
	if info, err := os.Stat(runtimeConfigPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("sing-box config mode = %v, %v, want 0600", info, err)
	}
	for _, directory := range []string{
		filepath.Join(projectRoot, "config"),
		filepath.Join(projectRoot, "secrets"),
		filepath.Join(projectRoot, "state"),
	} {
		info, err := os.Stat(directory)
		if err != nil {
			t.Fatalf("stat protected directory: %v", err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("directory %s mode = %04o, want 0700", directory, info.Mode().Perm())
		}
	}
}

func TestInitializeProjectRefusesToOverwriteSingBoxConfiguration(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	runtimeConfigPath := filepath.Join(projectRoot, "config", "sing-box.json")
	if err := os.MkdirAll(filepath.Dir(runtimeConfigPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(runtimeConfigPath, []byte("sentinel\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := initializeProject(initializationOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x3c}, 512)),
	})
	if err == nil {
		t.Fatal("initializeProject() overwrote an existing sing-box configuration")
	}
	contents, readErr := os.ReadFile(runtimeConfigPath)
	if readErr != nil || string(contents) != "sentinel\n" {
		t.Fatalf("existing sing-box config = %q, %v", contents, readErr)
	}
	for _, path := range []string{
		filepath.Join(projectRoot, "config", "config.json"),
		filepath.Join(projectRoot, "state", "runtime.json"),
		filepath.Join(projectRoot, "secrets", "password.hash"),
		filepath.Join(projectRoot, "secrets", "management.password"),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("initialization created %s after refusal: %v", path, statErr)
		}
	}
}

func TestInitializeProjectRefusesToOverwriteRuntimeDeploymentState(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	runtimeStatePath := filepath.Join(projectRoot, "state", "runtime.json")
	if err := os.MkdirAll(filepath.Dir(runtimeStatePath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(runtimeStatePath, []byte("sentinel\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := initializeProject(initializationOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x39}, 512)),
	})
	if err == nil {
		t.Fatal("initializeProject() overwrote existing runtime state")
	}
	contents, readErr := os.ReadFile(runtimeStatePath)
	if readErr != nil || string(contents) != "sentinel\n" {
		t.Fatalf("existing runtime state = %q, %v", contents, readErr)
	}
	for _, path := range []string{
		filepath.Join(projectRoot, "config", "config.json"),
		filepath.Join(projectRoot, "secrets", "password.hash"),
		filepath.Join(projectRoot, "secrets", "management.password"),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("initialization created %s after refusal: %v", path, statErr)
		}
	}
}

func TestInitializeProjectRefusesToOverwritePlaintextPassword(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	plainPasswordPath := filepath.Join(projectRoot, "secrets", "management.password")
	if err := os.MkdirAll(filepath.Dir(plainPasswordPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(plainPasswordPath, []byte("sentinel\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := initializeProject(initializationOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x38}, 512)),
	})
	if err == nil {
		t.Fatal("initializeProject() overwrote an existing plaintext password")
	}
	contents, readErr := os.ReadFile(plainPasswordPath)
	if readErr != nil || string(contents) != "sentinel\n" {
		t.Fatalf("existing plaintext password = %q, %v", contents, readErr)
	}
	for _, path := range []string{
		filepath.Join(projectRoot, "config", "config.json"),
		filepath.Join(projectRoot, "secrets", "password.hash"),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("initialization created %s after refusal: %v", path, statErr)
		}
	}
}

func TestInitializeProjectRefusesToOverwriteExistingState(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	configPath := filepath.Join(projectRoot, "config", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(configPath, []byte("sentinel"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := initializeProject(initializationOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x36}, 512)),
	})
	if err == nil {
		t.Fatal("initializeProject() overwrote existing state")
	}
	contents, readErr := os.ReadFile(configPath)
	if readErr != nil || string(contents) != "sentinel" {
		t.Fatalf("existing config = %q, %v", contents, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(projectRoot, "secrets", "password.hash")); !os.IsNotExist(statErr) {
		t.Fatalf("initialization created password hash after refusal: %v", statErr)
	}
}

func TestRunInitCommandPrintsBootstrapCredentialsWithoutHash(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	var output bytes.Buffer
	err := runCommand([]string{"init"}, commandOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x37}, 512)),
		Output:      &output,
	})
	if err != nil {
		t.Fatalf("runCommand(init) error = %v", err)
	}
	for _, expected := range []string{"初始管理密碼：", "Web 路徑：/", "管理埠：34456"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("init output missing %q: %s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "$pbkdf2-sha256$") {
		t.Fatalf("init output leaked password hash: %s", output.String())
	}
}

func TestRunStatusCommandPrintsDualStackURLsAndRecoverablePassword(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	result, err := initializeProject(initializationOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x39}, 512)),
	})
	if err != nil {
		t.Fatalf("initializeProject() error = %v", err)
	}
	var output bytes.Buffer
	err = runCommand([]string{"status"}, commandOptions{
		ProjectRoot: projectRoot,
		Output:      &output,
		Addresses: func() ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("198.51.100.7"), netip.MustParseAddr("2001:db8::7")}, nil
		},
	})
	if err != nil {
		t.Fatalf("runCommand(status) error = %v", err)
	}
	for _, expected := range []string{
		"ipv4: http://198.51.100.7:34456" + result.WebPath,
		"ipv6: http://[2001:db8::7]:34456" + result.WebPath,
		"管理密碼：" + result.Password,
		"Web 路徑：" + result.WebPath,
		"管理埠：34456",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("status output missing %q: %s", expected, output.String())
		}
	}
}

func TestRunStatusCommandReportsMissingAddressesAndRejectsUnprotectedPassword(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	_, err := initializeProject(initializationOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x3a}, 512)),
	})
	if err != nil {
		t.Fatalf("initializeProject() error = %v", err)
	}
	var output bytes.Buffer
	err = runCommand([]string{"status"}, commandOptions{
		ProjectRoot: projectRoot,
		Output:      &output,
		Addresses:   func() ([]netip.Addr, error) { return nil, nil },
	})
	if err != nil {
		t.Fatalf("runCommand(status) without addresses error = %v", err)
	}
	for _, expected := range []string{"ipv4: {未獲取到}", "ipv6: {未獲取到}"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("status output missing %q: %s", expected, output.String())
		}
	}

	passwordPath := filepath.Join(projectRoot, "secrets", "management.password")
	if err := os.Chmod(passwordPath, 0o644); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	err = runCommand([]string{"status"}, commandOptions{
		ProjectRoot: projectRoot,
		Output:      &bytes.Buffer{},
		Addresses:   func() ([]netip.Addr, error) { return nil, nil },
	})
	if err == nil {
		t.Fatal("runCommand(status) accepted a group/world-readable plaintext password")
	}
}

func TestRunHealthURLCommandPrintsLoopbackEndpointOnly(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	result, err := initializeProject(initializationOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x3b}, 512)),
	})
	if err != nil {
		t.Fatalf("initializeProject() error = %v", err)
	}
	var output bytes.Buffer
	if err := runCommand([]string{"health-url"}, commandOptions{ProjectRoot: projectRoot, Output: &output}); err != nil {
		t.Fatalf("runCommand(health-url) error = %v", err)
	}
	want := "http://127.0.0.1:34456" + result.WebPath + "/healthz\n"
	if output.String() != want {
		t.Fatalf("health-url output = %q, want %q", output.String(), want)
	}
	if strings.Contains(output.String(), result.Password) {
		t.Fatal("health-url output leaked management password")
	}
}

func TestRunResetPasswordCommandReplacesProtectedCredentials(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	initial, err := initializeProject(initializationOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x3d}, 512)),
	})
	if err != nil {
		t.Fatalf("initializeProject() error = %v", err)
	}
	newPassword := "NewManagementPassword1234"
	var output bytes.Buffer
	err = runCommand([]string{"reset-password"}, commandOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x3e}, 64)),
		Input:       strings.NewReader(newPassword + "\n"),
		Output:      &output,
	})
	if err != nil {
		t.Fatalf("runCommand(reset-password) error = %v", err)
	}
	if output.String() != "管理密碼已重設："+newPassword+"\n" {
		t.Fatalf("reset output = %q", output.String())
	}
	assertStoredManagementPassword(t, projectRoot, newPassword, initial.Password)
}

func TestRunResetPasswordCommandGeneratesProtectedCredential(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	if _, err := initializeProject(initializationOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x3f}, 512)),
	}); err != nil {
		t.Fatalf("initializeProject() error = %v", err)
	}
	var output bytes.Buffer
	err := runCommand([]string{"reset-password", "--generate"}, commandOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x40}, 128)),
		Output:      &output,
	})
	if err != nil {
		t.Fatalf("runCommand(reset-password --generate) error = %v", err)
	}
	match := regexp.MustCompile(`^管理密碼已重設：([A-Za-z0-9]{24})\n$`).FindStringSubmatch(output.String())
	if len(match) != 2 {
		t.Fatalf("generated reset output = %q", output.String())
	}
	assertStoredManagementPassword(t, projectRoot, match[1], "")
}

func TestRunResetPasswordCommandRejectsUnsafeCustomPasswordWithoutChanges(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	initial, err := initializeProject(initializationOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x41}, 512)),
	})
	if err != nil {
		t.Fatalf("initializeProject() error = %v", err)
	}
	hashPath := filepath.Join(projectRoot, "secrets", "password.hash")
	plainPath := filepath.Join(projectRoot, "secrets", "management.password")
	oldHash, _ := os.ReadFile(hashPath)
	oldPlain, _ := os.ReadFile(plainPath)

	err = runCommand([]string{"reset-password"}, commandOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x42}, 64)),
		Input:       strings.NewReader("short password!\n"),
		Output:      &bytes.Buffer{},
	})
	if err == nil || !strings.Contains(err.Error(), "至少 24 位英數字") {
		t.Fatalf("runCommand(reset-password) error = %v", err)
	}
	newHash, _ := os.ReadFile(hashPath)
	newPlain, _ := os.ReadFile(plainPath)
	if !bytes.Equal(newHash, oldHash) || !bytes.Equal(newPlain, oldPlain) {
		t.Fatalf("unsafe password changed credentials: hash=%t plain=%t", bytes.Equal(newHash, oldHash), bytes.Equal(newPlain, oldPlain))
	}
	assertStoredManagementPassword(t, projectRoot, initial.Password, "")
}

func TestResetManagementPasswordRollsBackBothFilesWhenCommitFails(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	initial, err := initializeProject(initializationOptions{
		ProjectRoot: projectRoot,
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x43}, 512)),
	})
	if err != nil {
		t.Fatalf("initializeProject() error = %v", err)
	}
	sentinel := errors.New("plaintext commit failed")
	failed := false
	_, err = resetManagementPassword(passwordResetOptions{
		ProjectRoot: projectRoot,
		Password:    "ReplacementPassword123456",
		Entropy:     bytes.NewReader(bytes.Repeat([]byte{0x44}, 64)),
		Rename: func(oldPath string, newPath string) error {
			if !failed && newPath == filepath.Join(projectRoot, "secrets", "management.password") {
				failed = true
				return sentinel
			}
			return os.Rename(oldPath, newPath)
		},
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("resetManagementPassword() error = %v, want sentinel", err)
	}
	assertStoredManagementPassword(t, projectRoot, initial.Password, "ReplacementPassword123456")
	matches, globErr := filepath.Glob(filepath.Join(projectRoot, "secrets", ".management-credentials.*"))
	if globErr != nil || len(matches) != 0 {
		t.Fatalf("credential temporary files = %#v, %v", matches, globErr)
	}
}

func assertStoredManagementPassword(t *testing.T, projectRoot string, expected string, rejected string) {
	t.Helper()
	hashPath := filepath.Join(projectRoot, "secrets", "password.hash")
	plainPath := filepath.Join(projectRoot, "secrets", "management.password")
	for _, path := range []string{hashPath, plainPath} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("protected credential %s mode = %v, %v", path, info, err)
		}
	}
	plain, err := readProtectedSecret(plainPath, "管理密碼")
	if err != nil || plain != expected {
		t.Fatalf("plaintext credential = %q, %v, want %q", plain, err, expected)
	}
	encoded, err := readProtectedPasswordHash(hashPath)
	if err != nil {
		t.Fatalf("read password hash: %v", err)
	}
	hasher := auth.NewPasswordHasher(nil)
	verified, err := hasher.Verify(encoded, expected)
	if err != nil || !verified {
		t.Fatalf("Verify(expected password) = %t, %v", verified, err)
	}
	if rejected != "" {
		verified, err = hasher.Verify(encoded, rejected)
		if err != nil || verified {
			t.Fatalf("Verify(rejected password) = %t, %v", verified, err)
		}
	}
}

func TestRunCleanupSystemCommandDelegatesToIntegrationCleaner(t *testing.T) {
	contextKey := struct{}{}
	ctx := context.WithValue(context.Background(), contextKey, "cleanup")
	cleaner := &stubIntegrationCleaner{}
	var output bytes.Buffer

	err := runCommand([]string{"cleanup-system"}, commandOptions{
		Context:            ctx,
		Output:             &output,
		IntegrationCleaner: cleaner,
	})
	if err != nil {
		t.Fatalf("runCommand(cleanup-system) error = %v", err)
	}
	if cleaner.calls != 1 || cleaner.contextValue != "cleanup" {
		t.Fatalf("cleaner calls = %d, context value = %q", cleaner.calls, cleaner.contextValue)
	}
	if output.String() != "專案網路整合狀態已清理。\n" {
		t.Fatalf("cleanup output = %q", output.String())
	}
}

func TestRunCleanupSystemCommandPreservesCleanupError(t *testing.T) {
	sentinel := errors.New("cleanup failed")
	cleaner := &stubIntegrationCleaner{err: sentinel}

	err := runCommand([]string{"cleanup-system"}, commandOptions{
		Context:            context.Background(),
		Output:             &bytes.Buffer{},
		IntegrationCleaner: cleaner,
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("runCommand(cleanup-system) error = %v, want sentinel", err)
	}
}

func TestRunRestoreSystemCommandDelegatesToIntegrationRestorer(t *testing.T) {
	contextKey := struct{}{}
	ctx := context.WithValue(context.Background(), contextKey, "restore")
	restorer := &stubIntegrationRestorer{}
	var output bytes.Buffer

	err := runCommand([]string{"restore-system"}, commandOptions{
		Context:             ctx,
		Output:              &output,
		IntegrationRestorer: restorer,
	})
	if err != nil {
		t.Fatalf("runCommand(restore-system) error = %v", err)
	}
	if restorer.calls != 1 || restorer.contextValue != "restore" {
		t.Fatalf("restorer calls = %d, context value = %q", restorer.calls, restorer.contextValue)
	}
	if output.String() != "專案網路整合狀態已恢復。\n" {
		t.Fatalf("restore output = %q", output.String())
	}
}

func TestRunRestoreSystemCommandPreservesRestoreError(t *testing.T) {
	sentinel := errors.New("restore failed")
	restorer := &stubIntegrationRestorer{err: sentinel}

	err := runCommand([]string{"restore-system"}, commandOptions{
		Context:             context.Background(),
		Output:              &bytes.Buffer{},
		IntegrationRestorer: restorer,
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("runCommand(restore-system) error = %v, want sentinel", err)
	}
}

func TestRunSetEndpointCommandPreservesProtectedFirewallScope(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	config := domain.DefaultConfig()
	config.Panel.AllowedCIDRs = []string{"198.51.100.0/24", "2001:db8::/32"}
	if err := store.NewConfigStore(filepath.Join(projectRoot, "config", "config.json")).Save(config); err != nil {
		t.Fatalf("Save(config) error = %v", err)
	}
	contextKey := struct{}{}
	ctx := context.WithValue(context.Background(), contextKey, "endpoint")
	updater := &stubEndpointUpdater{}
	var output bytes.Buffer

	err := runCommand([]string{"set-endpoint", "35555", "/newpanel1234", "2001:db8:100::20", "systemd"}, commandOptions{
		ProjectRoot:     projectRoot,
		Context:         ctx,
		Output:          &output,
		EndpointUpdater: updater,
	})
	if err != nil {
		t.Fatalf("runCommand(set-endpoint) error = %v", err)
	}
	want := domain.PanelConfig{
		Port:         35555,
		Path:         "/newpanel1234",
		ListenIPv6:   "2001:db8:100::20",
		AllowedCIDRs: []string{"198.51.100.0/24", "2001:db8::/32"},
	}
	if updater.calls != 1 || !reflect.DeepEqual(updater.panel, want) || updater.contextValue != "endpoint" {
		t.Fatalf("endpoint update = calls %d, panel %#v, context %q; want %#v", updater.calls, updater.panel, updater.contextValue, want)
	}
	if output.String() != "管理面板端點已更新。\n" {
		t.Fatalf("set-endpoint output = %q", output.String())
	}
}

func TestRunSetEndpointCommandAcceptsWildcardIPv6AndRejectsUnsafeInputs(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	if err := store.NewConfigStore(filepath.Join(projectRoot, "config", "config.json")).Save(domain.DefaultConfig()); err != nil {
		t.Fatalf("Save(config) error = %v", err)
	}
	updater := &stubEndpointUpdater{}
	if err := runCommand([]string{"set-endpoint", "34456", "/configureme1", "-", "openrc"}, commandOptions{
		ProjectRoot:     projectRoot,
		Output:          &bytes.Buffer{},
		EndpointUpdater: updater,
	}); err != nil {
		t.Fatalf("runCommand(set-endpoint wildcard) error = %v", err)
	}
	if updater.calls != 1 || updater.panel.ListenIPv6 != "" {
		t.Fatalf("wildcard endpoint update = calls %d, panel %#v", updater.calls, updater.panel)
	}

	tests := map[string][]string{
		"arguments":   {"set-endpoint", "34456"},
		"port":        {"set-endpoint", "0", "/configureme1", "-", "systemd"},
		"path":        {"set-endpoint", "34456", "unsafe/path", "-", "systemd"},
		"listener":    {"set-endpoint", "34456", "/configureme1", "198.51.100.2", "systemd"},
		"init system": {"set-endpoint", "34456", "/configureme1", "-", "unknown"},
	}
	for name, arguments := range tests {
			t.Run(name, func(t *testing.T) {
			before := updater.calls
			if err := runCommand(arguments, commandOptions{
				ProjectRoot:     projectRoot,
				Output:          &bytes.Buffer{},
				EndpointUpdater: updater,
			}); err == nil {
				t.Fatal("runCommand(set-endpoint) accepted unsafe input")
			}
			if updater.calls != before {
				t.Fatalf("updater calls = %d after rejected input, want %d", updater.calls, before)
			}
		})
	}
}

func TestRunSetEndpointCommandPreservesTransactionError(t *testing.T) {
	projectRoot := filepath.Join(t.TempDir(), "s12ryt-ipv6")
	if err := store.NewConfigStore(filepath.Join(projectRoot, "config", "config.json")).Save(domain.DefaultConfig()); err != nil {
		t.Fatalf("Save(config) error = %v", err)
	}
	sentinel := errors.New("endpoint transaction failed")
	updater := &stubEndpointUpdater{err: sentinel}
	err := runCommand([]string{"set-endpoint", "35555", "/newpanel1234", "-", "systemd"}, commandOptions{
		ProjectRoot:     projectRoot,
		Output:          &bytes.Buffer{},
		EndpointUpdater: updater,
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("runCommand(set-endpoint) error = %v, want sentinel", err)
	}
}

func TestLoadApplicationRejectsUnprotectedPasswordHash(t *testing.T) {
	paths := writeRuntimeFiles(t, 0o644)
	_, err := loadApplication(runtimeOptions{
		ConfigPath:       paths.config,
		PasswordHashPath: paths.passwordHash,
		RuntimeStatePath: paths.runtimeState,
	})
	if err == nil {
		t.Fatal("loadApplication accepted a group/world-readable password hash")
	}
}

func TestLoadApplicationRejectsMissingEnabledNodeDeployment(t *testing.T) {
	paths := writeRuntimeFiles(t, 0o600)
	config := domain.DefaultConfig()
	config.Nodes = []domain.Node{{
		ID:       "edge",
		Protocol: domain.ProtocolVLESS,
		Port:     24443,
		Enabled:  true,
		Credential: domain.NodeCredential{
			UUID: "123e4567-e89b-42d3-a456-426614174000",
		},
	}}
	if err := store.NewConfigStore(paths.config).Save(config); err != nil {
		t.Fatalf("Save(config) error = %v", err)
	}

	_, err := loadApplication(runtimeOptions{
		ConfigPath:       paths.config,
		PasswordHashPath: paths.passwordHash,
		RuntimeStatePath: paths.runtimeState,
		Entropy:          bytes.NewReader(bytes.Repeat([]byte{0x41}, 128)),
	})
	if err == nil || !strings.Contains(err.Error(), "edge") {
		t.Fatalf("loadApplication() error = %v, want missing edge deployment rejection", err)
	}
}

func TestLoadApplicationWiresManagedNodeCreationToPortChecker(t *testing.T) {
	paths := writeRuntimeFiles(t, 0o600)
	checker := &runtimePortChecker{}
	runtime := &stubRuntime{}
	validationCalls := 0
	application, err := loadApplication(runtimeOptions{
		ConfigPath:             paths.config,
		PasswordHashPath:       paths.passwordHash,
		RuntimeStatePath:       paths.runtimeState,
		RuntimeConfigPath:      paths.runtimeConfig,
		Entropy:                bytes.NewReader(bytes.Repeat([]byte{0x31}, 512)),
		Clock:                  func() time.Time { return time.Unix(1_800_000_000, 0) },
		PortChecker:            checker,
		PortAllocationAttempts: 4,
		Runtime:                runtime,
		ValidateRuntime: func(payload []byte) error {
			validationCalls++
			if !bytes.Contains(payload, []byte("runtime-vless")) {
				return errors.New("candidate runtime config missing node")
			}
			return nil
		},
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
		"deployment": map[string]any{
			"listeners": []string{"2001:db8::10"},
			"tls":       map[string]any{"enabled": false},
			"transport": map[string]any{},
		},
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
	if validationCalls != 1 || runtime.reloads != 1 || runtime.healthChecks != 1 {
		t.Fatalf("runtime validation/reload/health = %d/%d/%d, want 1/1/1", validationCalls, runtime.reloads, runtime.healthChecks)
	}
	storedConfig, err := store.NewConfigStore(paths.config).Load()
	if err != nil || len(storedConfig.Nodes) != 1 || storedConfig.Nodes[0].ID != "runtime-vless" {
		t.Fatalf("stored configuration = %#v, %v", storedConfig, err)
	}
	stateStore, err := runtimeconfig.NewDeploymentStateStore(paths.runtimeState)
	if err != nil {
		t.Fatal(err)
	}
	storedState, err := stateStore.Load()
	if err != nil || len(storedState.Nodes) != 1 || storedState.Nodes[0].NodeID != "runtime-vless" {
		t.Fatalf("stored runtime state = %#v, %v", storedState, err)
	}
}

func TestLoadApplicationWiresManagedRemoteOutboundAPI(t *testing.T) {
	paths := writeRuntimeFiles(t, 0o600)
	application, err := loadApplication(runtimeOptions{
		ConfigPath:        paths.config,
		PasswordHashPath:  paths.passwordHash,
		RuntimeStatePath:  paths.runtimeState,
		RuntimeConfigPath: paths.runtimeConfig,
		Entropy:           bytes.NewReader(bytes.Repeat([]byte{0x4d}, 256)),
		Clock:             func() time.Time { return time.Unix(1_800_000_000, 0) },
		Runtime:           &stubRuntime{},
		ValidateRuntime:   func([]byte) error { return nil },
	})
	if err != nil {
		t.Fatalf("loadApplication returned error: %v", err)
	}

	cookie := runtimeLogin(t, application.handler)
	request := httptest.NewRequest(http.MethodGet, "/configureme1/api/remotes", nil)
	request.RemoteAddr = "198.51.100.20:43210"
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	application.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("remote list response = %d %q, want 200 []", response.Code, response.Body.String())
	}
}

func TestLoadApplicationWiresNetworkIntegrationAPI(t *testing.T) {
	paths := writeRuntimeFiles(t, 0o600)
	manager := &stubNetworkManager{addresses: []projectnetwork.InterfaceAddress{{
		Interface: "eth0",
		Prefix:    netip.MustParsePrefix("2001:db8::1/64"),
	}}}
	application, err := loadApplication(runtimeOptions{
		ConfigPath:        paths.config,
		PasswordHashPath:  paths.passwordHash,
		RuntimeStatePath:  paths.runtimeState,
		RuntimeConfigPath: paths.runtimeConfig,
		Entropy:           bytes.NewReader(bytes.Repeat([]byte{0x5d}, 256)),
		Clock:             func() time.Time { return time.Unix(1_800_000_000, 0) },
		Runtime:           &stubRuntime{},
		ValidateRuntime:   func([]byte) error { return nil },
		NetworkManager:    manager,
	})
	if err != nil {
		t.Fatalf("loadApplication returned error: %v", err)
	}

	cookie := runtimeLogin(t, application.handler)
	request := httptest.NewRequest(http.MethodGet, "/configureme1/api/network/addresses", nil)
	request.RemoteAddr = "198.51.100.20:43210"
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	application.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"interface":"eth0"`) {
		t.Fatalf("network list response = %d %q, want wired manager", response.Code, response.Body.String())
	}
}

func TestLoadApplicationWiresACMEHTTPPortPreflight(t *testing.T) {
	paths := writeRuntimeFiles(t, 0o600)
	checker := &stubACMEChallengeChecker{available: true}
	application, err := loadApplication(runtimeOptions{
		ConfigPath:           paths.config,
		PasswordHashPath:     paths.passwordHash,
		RuntimeStatePath:     paths.runtimeState,
		RuntimeConfigPath:    paths.runtimeConfig,
		Entropy:              bytes.NewReader(bytes.Repeat([]byte{0x6d}, 512)),
		Clock:                func() time.Time { return time.Unix(1_800_000_000, 0) },
		Runtime:              &stubRuntime{},
		ValidateRuntime:      func([]byte) error { return nil },
		ACMEChallengeChecker: checker,
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

	payload := []byte(`{"id":"acme-edge","protocol":"vless","port":25555,"enabled":true,"deployment":{"listeners":["2001:db8::20"],"tls":{"enabled":true,"server_name":"node.example.com","acme":{"domains":["node.example.com"],"data_directory":"/opt/s12ryt-ipv6/tls/acme","default_server_name":"node.example.com","email":"admin@example.com","provider":"letsencrypt"}},"transport":{}}}`)
	created := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/configureme1/api/nodes", bytes.NewReader(payload))
	createRequest.RemoteAddr = "198.51.100.20:43210"
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("X-CSRF-Token", csrfMatch[1])
	createRequest.Header.Set("X-S12ryt-Confirm", "apply")
	createRequest.AddCookie(cookie)
	application.handler.ServeHTTP(created, createRequest)
	if created.Code != http.StatusCreated || checker.calls != 1 {
		t.Fatalf("ACME create response = %d %q, checker calls = %d", created.Code, created.Body.String(), checker.calls)
	}
}

func TestRunApplicationListensOnDualStackAndShutsDownGracefully(t *testing.T) {
	paths := writeRuntimeFiles(t, 0o600)
	listener := &stubListener{}
	server := newStubHTTPServer()
	runtime := &stubRuntime{}
	var network, address string
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- runApplication(ctx, runtimeOptions{
			ConfigPath:        paths.config,
			PasswordHashPath:  paths.passwordHash,
			RuntimeStatePath:  paths.runtimeState,
			RuntimeConfigPath: paths.runtimeConfig,
			Entropy:           bytes.NewReader(bytes.Repeat([]byte{0x42}, 128)),
			Clock:             func() time.Time { return time.Unix(1_800_000_000, 0) },
			Runtime:           runtime,
			ValidateRuntime:   func([]byte) error { return nil },
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
	if runtime.starts != 1 {
		t.Fatalf("runtime starts = %d, want 1", runtime.starts)
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
	if runtime.stops != 1 {
		t.Fatalf("runtime stops = %d, want 1", runtime.stops)
	}
}

func TestRunApplicationListensOnIPv4AndConfiguredIPv6(t *testing.T) {
	paths := writeRuntimeFiles(t, 0o600)
	configStore := store.NewConfigStore(paths.config)
	config, err := configStore.Load()
	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}
	config.Panel.ListenIPv6 = "2001:db8:100::20"
	if err := configStore.Save(config); err != nil {
		t.Fatalf("save configuration: %v", err)
	}

	listeners := []*stubListener{{}, {}}
	servers := []*stubHTTPServer{newStubHTTPServer(), newStubHTTPServer()}
	runtime := &stubRuntime{}
	addresses := make([]string, 0, 2)
	serverIndex := 0
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- runApplication(ctx, runtimeOptions{
			ConfigPath:        paths.config,
			PasswordHashPath:  paths.passwordHash,
			RuntimeStatePath:  paths.runtimeState,
			RuntimeConfigPath: paths.runtimeConfig,
			Entropy:           bytes.NewReader(bytes.Repeat([]byte{0x43}, 128)),
			Clock:             func() time.Time { return time.Unix(1_800_000_000, 0) },
			Runtime:           runtime,
			ValidateRuntime:   func([]byte) error { return nil },
			Listen: func(network string, address string) (net.Listener, error) {
				if network != "tcp" {
					return nil, fmt.Errorf("unexpected network %q", network)
				}
				if len(addresses) >= len(listeners) {
					return nil, errors.New("too many listener calls")
				}
				addresses = append(addresses, address)
				return listeners[len(addresses)-1], nil
			},
			NewHTTPServer: func(_ string, _ http.Handler) managedHTTPServer {
				if serverIndex >= len(servers) {
					return nil
				}
				server := servers[serverIndex]
				serverIndex++
				return server
			},
		})
	}()

	for index, server := range servers {
		select {
		case <-server.started:
		case <-time.After(time.Second):
			cancel()
			<-done
			t.Fatalf("HTTP server %d did not start", index)
		}
	}
	if len(addresses) != 2 || addresses[0] != "0.0.0.0:34456" || addresses[1] != "[2001:db8:100::20]:34456" {
		t.Fatalf("listen addresses = %#v", addresses)
	}
	if runtime.starts != 1 {
		t.Fatalf("runtime starts = %d, want 1", runtime.starts)
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
	for index, server := range servers {
		if !server.wasShutdown() {
			t.Fatalf("HTTP server %d was not shut down gracefully", index)
		}
	}
	if runtime.stops != 1 {
		t.Fatalf("runtime stops = %d, want 1", runtime.stops)
	}
}

type runtimePaths struct {
	config        string
	passwordHash  string
	runtimeState  string
	runtimeConfig string
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
	runtimeStatePath := filepath.Join(directory, "runtime.json")
	runtimeStateStore, err := runtimeconfig.NewDeploymentStateStore(runtimeStatePath)
	if err != nil {
		t.Fatalf("NewDeploymentStateStore() error = %v", err)
	}
	initialState := runtimeconfig.DeploymentState{
		SchemaVersion: runtimeconfig.DeploymentStateSchemaVersion,
		Nodes:         make([]runtimeconfig.PersistedNodeDeployment, 0),
		IPv6Outbounds: make([]netip.Addr, 0),
	}
	if err := runtimeStateStore.Save(initialState); err != nil {
		t.Fatalf("Save(runtime state) error = %v", err)
	}
	runtimeInput, err := initialState.Resolve(domain.DefaultConfig())
	if err != nil {
		t.Fatalf("Resolve(runtime state) error = %v", err)
	}
	compiledConfig, err := runtimeconfig.CompileServerConfig(runtimeInput)
	if err != nil {
		t.Fatalf("CompileServerConfig() error = %v", err)
	}
	runtimeConfigPath := filepath.Join(directory, "sing-box.json")
	if err := os.WriteFile(runtimeConfigPath, compiledConfig, 0o600); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}
	return runtimePaths{
		config:        configPath,
		passwordHash:  passwordPath,
		runtimeState:  runtimeStatePath,
		runtimeConfig: runtimeConfigPath,
	}
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

type stubNetworkManager struct {
	addresses []projectnetwork.InterfaceAddress
}

type stubACMEChallengeChecker struct {
	available bool
	err       error
	calls     int
}

func (checker *stubACMEChallengeChecker) Available(context.Context) (bool, error) {
	checker.calls++
	return checker.available, checker.err
}

func (manager *stubNetworkManager) GlobalIPv6Addresses(context.Context) ([]projectnetwork.InterfaceAddress, error) {
	return append([]projectnetwork.InterfaceAddress(nil), manager.addresses...), nil
}

func (manager *stubNetworkManager) Apply(_ context.Context, request networksetup.Request) (manifest.Manifest, error) {
	return manifest.Manifest{SchemaVersion: manifest.SchemaVersion, Interface: request.Interface}, nil
}

var _ panel.NetworkManager = (*stubNetworkManager)(nil)

type stubRuntime struct {
	starts       int
	reloads      int
	healthChecks int
	stops        int
}

type stubIntegrationCleaner struct {
	calls        int
	contextValue string
	err          error
}

type stubIntegrationRestorer struct {
	calls        int
	contextValue string
	err          error
}

type stubEndpointUpdater struct {
	calls        int
	panel        domain.PanelConfig
	contextValue string
	err          error
}

func (updater *stubEndpointUpdater) Apply(ctx context.Context, panel domain.PanelConfig) error {
	updater.calls++
	updater.panel = panel
	updater.contextValue, _ = ctx.Value(struct{}{}).(string)
	return updater.err
}

var _ endpoint.Repository = (*store.ConfigStore)(nil)

func (restorer *stubIntegrationRestorer) Restore(ctx context.Context) error {
	restorer.calls++
	if value, ok := ctx.Value(struct{}{}).(string); ok {
		restorer.contextValue = value
	}
	return restorer.err
}

func (cleaner *stubIntegrationCleaner) Remove(ctx context.Context) error {
	cleaner.calls++
	if value, ok := ctx.Value(struct{}{}).(string); ok {
		cleaner.contextValue = value
	}
	return cleaner.err
}

func (runtime *stubRuntime) Start() error {
	runtime.starts++
	return nil
}

func (runtime *stubRuntime) Reload(context.Context) error {
	runtime.reloads++
	return nil
}

func (runtime *stubRuntime) Healthy(context.Context) error {
	runtime.healthChecks++
	return nil
}

func (runtime *stubRuntime) Stop(context.Context) error {
	runtime.stops++
	return nil
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
