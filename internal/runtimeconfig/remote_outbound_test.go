package runtimeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

func TestDeploymentStatePersistsRemoteSecretsAndCompilesRotatingOutbound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "runtime.json")
	store, err := NewDeploymentStateStore(path)
	if err != nil {
		t.Fatalf("NewDeploymentStateStore() error = %v", err)
	}
	state := deploymentStateFixture()
	state.RemoteOutbounds = []PersistedRemoteOutbound{{
		Enabled: true,
		Config:  json.RawMessage(`{"type":"vless","tag":"remote-vless","server":"proxy.example.com","server_port":443,"uuid":"550e8400-e29b-41d4-a716-446655440000","tls":{"enabled":true,"server_name":"proxy.example.com"}}`),
	}}
	if err := store.Save(state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	assertFileMode(t, path, 0o600)
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(stored), "550e8400-e29b-41d4-a716-446655440000") {
		t.Fatalf("protected runtime state omitted remote credential: %s", stored)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	config := configuredRuntimeConfig()
	config.Routing.Topology = domain.TopologyMultiIPv6RotatingNode
	input, err := loaded.Resolve(config)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	payload, err := CompileServerConfig(input)
	if err != nil {
		t.Fatalf("CompileServerConfig() error = %v", err)
	}
	text := string(payload)
	for _, required := range []string{
		`"tag": "remote-vless"`,
		`"tag": "rotate-edge"`,
		`"outbounds": [`,
		`"remote-vless"`,
		`"interrupt_exist_connections": false`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("compiled remote runtime missing %s: %s", required, text)
		}
	}
}

func TestDeploymentStateCompilesOrderedIPv4ProxyFallback(t *testing.T) {
	state := deploymentStateFixture()
	state.RemoteOutbounds = []PersistedRemoteOutbound{{
		Config: json.RawMessage(`{"type":"socks","tag":"remote-socks","server":"192.0.2.10","server_port":1080,"version":"5","username":"user","password":"secret"}`),
	}}
	state.IPv4Fallback = []string{"remote-socks", "direct-v4"}
	config := configuredRuntimeConfig()
	config.Routing.Mode = domain.RoutingModeVPSIPv4

	input, err := state.Resolve(config)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	payload, err := CompileServerConfig(input)
	if err != nil {
		t.Fatalf("CompileServerConfig() error = %v", err)
	}
	var decoded struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	var selector map[string]any
	for _, outbound := range decoded.Outbounds {
		if outbound["tag"] == "select-ipv4" {
			selector = outbound
		}
	}
	if selector == nil {
		t.Fatalf("IPv4 selector missing from %#v", decoded.Outbounds)
	}
	candidates, ok := selector["outbounds"].([]any)
	if !ok || len(candidates) != 2 || candidates[0] != "remote-socks" || candidates[1] != "direct-v4" {
		t.Fatalf("IPv4 fallback order = %#v", selector["outbounds"])
	}
}

func TestDeploymentStateRejectsUnsafeRemoteOutboundReferences(t *testing.T) {
	validVLESS := PersistedRemoteOutbound{
		Enabled: true,
		Config:  json.RawMessage(`{"type":"vless","tag":"remote-vless","server":"proxy.example.com","server_port":443,"uuid":"550e8400-e29b-41d4-a716-446655440000"}`),
	}
	validSOCKS := PersistedRemoteOutbound{
		Config: json.RawMessage(`{"type":"socks","tag":"remote-socks","server":"192.0.2.10","server_port":1080,"version":"5","username":"user","password":"secret"}`),
	}
	for name, mutate := range map[string]func(*DeploymentState){
		"unsupported outbound": func(state *DeploymentState) {
			state.RemoteOutbounds = []PersistedRemoteOutbound{{Config: json.RawMessage(`{"type":"direct","tag":"unsafe","server":"example.com","server_port":443}`)}}
		},
		"unsafe tag": func(state *DeploymentState) {
			state.RemoteOutbounds = []PersistedRemoteOutbound{{Config: json.RawMessage(`{"type":"vless","tag":"../escape","server":"example.com","server_port":443,"uuid":"550e8400-e29b-41d4-a716-446655440000"}`)}}
		},
		"duplicate tag": func(state *DeploymentState) {
			state.RemoteOutbounds = []PersistedRemoteOutbound{validVLESS, validVLESS}
		},
		"unknown fallback": func(state *DeploymentState) {
			state.RemoteOutbounds = []PersistedRemoteOutbound{validSOCKS}
			state.IPv4Fallback = []string{"missing"}
		},
		"non IPv4 proxy fallback": func(state *DeploymentState) {
			state.RemoteOutbounds = []PersistedRemoteOutbound{validVLESS}
			state.IPv4Fallback = []string{"remote-vless"}
		},
		"duplicate fallback": func(state *DeploymentState) {
			state.RemoteOutbounds = []PersistedRemoteOutbound{validSOCKS}
			state.IPv4Fallback = []string{"remote-socks", "remote-socks"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			state := deploymentStateFixture()
			mutate(&state)
			if err := state.Validate(); err == nil {
				t.Fatal("Validate() accepted unsafe remote outbound state")
			}
		})
	}
}
