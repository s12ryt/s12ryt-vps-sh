package runtimeconfig

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/singbox"
)

func TestCompileServerConfigMapsEnabledNodesAndResolvedDeployment(t *testing.T) {
	config := domain.DefaultConfig()
	config.Nodes = []domain.Node{
		{
			ID:       "edge-vless",
			Protocol: domain.ProtocolVLESS,
			Port:     24443,
			Enabled:  true,
			Credential: domain.NodeCredential{
				UUID: "123e4567-e89b-42d3-a456-426614174000",
			},
		},
		{
			ID:       "disabled-node",
			Protocol: domain.ProtocolAnyTLS,
			Port:     24444,
			Enabled:  false,
			Credential: domain.NodeCredential{
				Password: "abcdefghijklmnopqrstuvwx",
			},
		},
	}
	input := Input{
		Config: config,
		Deployments: []NodeDeployment{
			{
				NodeID:    "edge-vless",
				Listeners: []netip.Addr{netip.MustParseAddr("198.51.100.7"), netip.MustParseAddr("2001:db8::7")},
				TLS: singbox.TLSConfig{
					Enabled:         true,
					ServerName:      "edge.example.com",
					CertificatePath: "/opt/s12ryt-ipv6/tls/server.crt",
					KeyPath:         "/opt/s12ryt-ipv6/tls/server.key",
				},
				Transport: singbox.TransportConfig{Type: singbox.TransportWebSocket, Path: "/edge"},
			},
		},
		IPv6Outbounds: []netip.Addr{netip.MustParseAddr("2001:db8:1::10"), netip.MustParseAddr("2001:db8:1::11")},
	}

	payload, err := CompileServerConfig(input)
	if err != nil {
		t.Fatalf("CompileServerConfig() error = %v", err)
	}
	var decoded struct {
		Inbounds  []map[string]any `json:"inbounds"`
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("generated JSON is invalid: %v", err)
	}
	if len(decoded.Inbounds) != 2 {
		t.Fatalf("inbound count = %d, want 2 dual-stack listeners", len(decoded.Inbounds))
	}
	if len(decoded.Outbounds) != 2 {
		t.Fatalf("outbound count = %d, want 2", len(decoded.Outbounds))
	}
	text := string(payload)
	for _, required := range []string{
		`"tag": "in-edge-vless-v4"`,
		`"tag": "in-edge-vless-v6"`,
		`"inet6_bind_address": "2001:db8:1::10"`,
		`"type": "ws"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated config missing %s: %s", required, text)
		}
	}
	if strings.Contains(text, "disabled-node") {
		t.Fatalf("disabled node leaked into generated config: %s", text)
	}
}

func TestCompileServerConfigRejectsMissingAndUnknownDeployments(t *testing.T) {
	config := domain.DefaultConfig()
	credential := domain.NodeCredential{UUID: "123e4567-e89b-42d3-a456-426614174000"}
	config.Nodes = []domain.Node{{
		ID: "required", Protocol: domain.ProtocolVLESS, Port: 24443, Enabled: true, Credential: credential,
	}}

	if _, err := CompileServerConfig(Input{Config: config}); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("missing deployment error = %v", err)
	}
	_, err := CompileServerConfig(Input{
		Config: config,
		Deployments: []NodeDeployment{{
			NodeID: "unknown", Listeners: []netip.Addr{netip.MustParseAddr("2001:db8::9")},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown deployment error = %v", err)
	}
}

func TestCompileServerConfigEnforcesSingleNodeTopology(t *testing.T) {
	config := domain.DefaultConfig()
	config.Routing.Topology = domain.TopologySingleIPv6SingleNode
	credential := domain.NodeCredential{UUID: "123e4567-e89b-42d3-a456-426614174000"}
	config.Nodes = []domain.Node{
		{ID: "one", Protocol: domain.ProtocolVLESS, Port: 24443, Enabled: true, Credential: credential},
		{ID: "two", Protocol: domain.ProtocolVMess, Port: 24444, Enabled: true, Credential: credential},
	}
	input := Input{
		Config: config,
		Deployments: []NodeDeployment{
			{NodeID: "one", Listeners: []netip.Addr{netip.MustParseAddr("2001:db8::1")}},
			{NodeID: "two", Listeners: []netip.Addr{netip.MustParseAddr("2001:db8::2")}},
		},
		IPv6Outbounds: []netip.Addr{netip.MustParseAddr("2001:db8:1::1")},
	}

	if _, err := CompileServerConfig(input); err == nil || !strings.Contains(err.Error(), "single") {
		t.Fatalf("single-node topology error = %v", err)
	}
}
