package runtimeconfig

import (
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDeploymentStateStoreSavesLoadsProtectedStateAndResolvesInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "runtime.json")
	stateStore, err := NewDeploymentStateStore(path)
	if err != nil {
		t.Fatalf("NewDeploymentStateStore() error = %v", err)
	}
	state := deploymentStateFixture()
	if err := stateStore.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := stateStore.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(loaded, state) {
		t.Fatalf("Load() = %#v, want %#v", loaded, state)
	}
	assertFileMode(t, filepath.Dir(path), 0o700)
	assertFileMode(t, path, 0o600)

	config := configuredRuntimeConfig()
	input, err := loaded.Resolve(config)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	payload, err := CompileServerConfig(input)
	if err != nil {
		t.Fatalf("CompileServerConfig() error = %v", err)
	}
	for _, required := range []string{
		`"listen": "198.51.100.7"`,
		`"listen": "2001:db8::7"`,
		`"inet6_bind_address": "2001:db8:1::10"`,
		`"type": "ws"`,
	} {
		if !strings.Contains(string(payload), required) {
			t.Fatalf("compiled payload missing %s: %s", required, payload)
		}
	}
}

func TestDeploymentStateStoreKeepsBackupAndRejectsInvalidReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.json")
	stateStore, err := NewDeploymentStateStore(path)
	if err != nil {
		t.Fatalf("NewDeploymentStateStore() error = %v", err)
	}
	original := deploymentStateFixture()
	if err := stateStore.Save(original); err != nil {
		t.Fatalf("Save(original) error = %v", err)
	}

	updated := deploymentStateFixture()
	updated.IPv6Outbounds = []netip.Addr{netip.MustParseAddr("2001:db8:1::11")}
	if err := stateStore.Save(updated); err != nil {
		t.Fatalf("Save(updated) error = %v", err)
	}
	backupStore, err := NewDeploymentStateStore(path + ".bak")
	if err != nil {
		t.Fatalf("NewDeploymentStateStore(backup) error = %v", err)
	}
	backup, err := backupStore.Load()
	if err != nil {
		t.Fatalf("Load(backup) error = %v", err)
	}
	if !reflect.DeepEqual(backup, original) {
		t.Fatalf("backup = %#v, want original %#v", backup, original)
	}

	invalid := updated
	invalid.SchemaVersion = 99
	if err := stateStore.Save(invalid); err == nil {
		t.Fatal("Save(invalid) error = nil, want rejection")
	}
	current, err := stateStore.Load()
	if err != nil {
		t.Fatalf("Load(current) error = %v", err)
	}
	if !reflect.DeepEqual(current, updated) {
		t.Fatalf("current state changed after rejected save: %#v", current)
	}
}

func TestDeploymentStateStoreRoundTripsACMEHTTP01Configuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "runtime.json")
	stateStore, err := NewDeploymentStateStore(path)
	if err != nil {
		t.Fatalf("NewDeploymentStateStore() error = %v", err)
	}
	state := deploymentStateFixture()
	state.Nodes[0].TLS = PersistedTLSConfig{
		Enabled:    true,
		ServerName: "node.example.com",
		ACME: &PersistedACMEConfig{
			Domains:           []string{"node.example.com"},
			DataDirectory:     "/opt/s12ryt-ipv6/tls/acme",
			DefaultServerName: "node.example.com",
			Email:             "admin@example.com",
			Provider:          "letsencrypt",
		},
	}
	if err := stateStore.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := stateStore.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(loaded, state) {
		t.Fatalf("Load() = %#v, want %#v", loaded, state)
	}
	input, err := loaded.Resolve(configuredRuntimeConfig())
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	payload, err := CompileServerConfig(input)
	if err != nil {
		t.Fatalf("CompileServerConfig() error = %v", err)
	}
	for _, required := range []string{
		`"acme"`,
		`"domain": [`,
		`"node.example.com"`,
		`"data_directory": "/opt/s12ryt-ipv6/tls/acme"`,
		`"disable_http_challenge": false`,
		`"disable_tls_alpn_challenge": true`,
	} {
		if !strings.Contains(string(payload), required) {
			t.Fatalf("compiled ACME payload missing %s: %s", required, payload)
		}
	}
	assertFileMode(t, path, 0o600)
}

func TestDeploymentStateStoreRejectsUnknownJSONFields(t *testing.T) {
	for name, payload := range map[string]string{
		"top level":  `{"schema_version":1,"nodes":[],"ipv6_outbounds":[],"unknown":true}`,
		"nested TLS": `{"schema_version":1,"nodes":[{"node_id":"edge","listeners":["2001:db8::7"],"tls":{"enabled":false,"unknown":true},"transport":{}}],"ipv6_outbounds":["2001:db8:1::10"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.json")
			if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			stateStore, err := NewDeploymentStateStore(path)
			if err != nil {
				t.Fatalf("NewDeploymentStateStore() error = %v", err)
			}
			if _, err := stateStore.Load(); err == nil {
				t.Fatal("Load() error = nil, want unknown field rejection")
			}
		})
	}
}

func TestDeploymentStateStoreRejectsUnprotectedAndLinkedState(t *testing.T) {
	for name, arrange := range map[string]func(*testing.T, string){
		"group readable": func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte(`{"schema_version":1,"nodes":[],"ipv6_outbounds":[]}`), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
		},
		"symbolic link": func(t *testing.T, path string) {
			t.Helper()
			target := path + ".target"
			if err := os.WriteFile(target, []byte(`{"schema_version":1,"nodes":[],"ipv6_outbounds":[]}`), 0o600); err != nil {
				t.Fatalf("WriteFile(target) error = %v", err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatalf("Symlink() error = %v", err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.json")
			arrange(t, path)
			stateStore, err := NewDeploymentStateStore(path)
			if err != nil {
				t.Fatalf("NewDeploymentStateStore() error = %v", err)
			}
			if _, err := stateStore.Load(); err == nil {
				t.Fatal("Load() error = nil, want protected regular file rejection")
			}
		})
	}
}

func TestDeploymentStateValidationRejectsUnsafeState(t *testing.T) {
	for name, mutate := range map[string]func(*DeploymentState){
		"schema":  func(state *DeploymentState) { state.SchemaVersion = 2 },
		"node ID": func(state *DeploymentState) { state.Nodes[0].NodeID = "../edge" },
		"duplicate node": func(state *DeploymentState) {
			state.Nodes = append(state.Nodes, state.Nodes[0])
		},
		"link local listener": func(state *DeploymentState) {
			state.Nodes[0].Listeners = []netip.Addr{netip.MustParseAddr("fe80::1")}
		},
		"duplicate listener family": func(state *DeploymentState) {
			state.Nodes[0].Listeners = []netip.Addr{
				netip.MustParseAddr("2001:db8::7"),
				netip.MustParseAddr("2001:db8::8"),
			}
		},
		"IPv4 outbound": func(state *DeploymentState) {
			state.IPv6Outbounds = []netip.Addr{netip.MustParseAddr("198.51.100.8")}
		},
		"duplicate outbound": func(state *DeploymentState) {
			state.IPv6Outbounds = []netip.Addr{
				netip.MustParseAddr("2001:db8:1::10"),
				netip.MustParseAddr("2001:db8:1::10"),
			}
		},
		"unknown transport": func(state *DeploymentState) {
			state.Nodes[0].Transport.Type = "quic"
		},
		"ACME and Reality": func(state *DeploymentState) {
			state.Nodes[0].TLS = PersistedTLSConfig{
				Enabled: true,
				ACME: &PersistedACMEConfig{
					Domains:       []string{"node.example.com"},
					DataDirectory: "/opt/s12ryt-ipv6/tls/acme",
				},
				Reality: &PersistedRealityConfig{
					HandshakeServer: "node.example.com",
					HandshakePort:   443,
					PrivateKey:      "private-key",
					ShortID:         "0123456789abcdef",
				},
			}
		},
		"ACME unsafe directory": func(state *DeploymentState) {
			state.Nodes[0].TLS = PersistedTLSConfig{
				Enabled: true,
				ACME: &PersistedACMEConfig{
					Domains:       []string{"node.example.com"},
					DataDirectory: "/tmp/acme",
				},
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			state := deploymentStateFixture()
			mutate(&state)
			if err := state.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want rejection")
			}
		})
	}
}

func TestDeploymentStateResolveRejectsMissingAndUnknownNodeBindings(t *testing.T) {
	config := configuredRuntimeConfig()
	missing := DeploymentState{
		SchemaVersion: DeploymentStateSchemaVersion,
		IPv6Outbounds: []netip.Addr{netip.MustParseAddr("2001:db8:1::10")},
	}
	if _, err := missing.Resolve(config); err == nil || !strings.Contains(err.Error(), "edge") {
		t.Fatalf("Resolve(missing) error = %v, want edge binding rejection", err)
	}

	unknown := deploymentStateFixture()
	unknown.Nodes[0].NodeID = "unknown"
	if _, err := unknown.Resolve(config); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("Resolve(unknown) error = %v, want unknown binding rejection", err)
	}
}

func deploymentStateFixture() DeploymentState {
	return DeploymentState{
		SchemaVersion: DeploymentStateSchemaVersion,
		Nodes: []PersistedNodeDeployment{{
			NodeID:    "edge",
			Listeners: []netip.Addr{netip.MustParseAddr("198.51.100.7"), netip.MustParseAddr("2001:db8::7")},
			TLS: PersistedTLSConfig{
				Enabled:         true,
				ServerName:      "edge.example.com",
				CertificatePath: "/opt/s12ryt-ipv6/tls/server.crt",
				KeyPath:         "/opt/s12ryt-ipv6/tls/server.key",
			},
			Transport: PersistedTransportConfig{Type: "websocket", Path: "/edge"},
		}},
		IPv6Outbounds: []netip.Addr{netip.MustParseAddr("2001:db8:1::10")},
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode(%s) = %04o, want %04o", path, got, want)
	}
}
