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
		Route     struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("generated JSON is invalid: %v", err)
	}
	if len(decoded.Inbounds) != 2 {
		t.Fatalf("inbound count = %d, want 2 dual-stack listeners", len(decoded.Inbounds))
	}
	if len(decoded.Outbounds) != 3 {
		t.Fatalf("outbound count = %d, want two IPv6 and one IPv4 direct outbound", len(decoded.Outbounds))
	}
	if len(decoded.Route.Rules) != 2 || decoded.Route.Rules[0]["outbound"] != "direct-v6-1" || decoded.Route.Rules[1]["outbound"] != "direct-v4" {
		t.Fatalf("runtime route rules = %#v", decoded.Route.Rules)
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

func TestCompileServerConfigBuildsRotatingSelectorsWithoutInterruptingExistingConnections(t *testing.T) {
	config := domain.DefaultConfig()
	config.Routing.Mode = domain.RoutingModeIPv6Only
	config.Routing.Topology = domain.TopologyMultiIPv6RotatingNode
	config.Nodes = []domain.Node{{
		ID:       "rotating",
		Protocol: domain.ProtocolVLESS,
		Port:     24443,
		Enabled:  true,
		Credential: domain.NodeCredential{
			UUID: "123e4567-e89b-42d3-a456-426614174000",
		},
	}}
	payload, err := CompileServerConfig(Input{
		Config: config,
		Deployments: []NodeDeployment{{
			NodeID:    "rotating",
			Listeners: []netip.Addr{netip.MustParseAddr("2001:db8::10")},
		}},
		IPv6Outbounds: []netip.Addr{
			netip.MustParseAddr("2001:db8:1::10"),
			netip.MustParseAddr("2001:db8:1::11"),
		},
	})
	if err != nil {
		t.Fatalf("CompileServerConfig() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("generated JSON is invalid: %v", err)
	}
	outbounds := decoded["outbounds"].([]any)
	selector := outbounds[2].(map[string]any)
	if selector["type"] != "selector" || selector["tag"] != "rotate-rotating" || selector["interrupt_exist_connections"] != false {
		t.Fatalf("rotation selector = %#v", selector)
	}
	rules := decoded["route"].(map[string]any)["rules"].([]any)
	if rules[0].(map[string]any)["outbound"] != "rotate-rotating" || rules[1].(map[string]any)["action"] != "reject" {
		t.Fatalf("rotation route rules = %#v", rules)
	}
}

func TestCompileServerConfigDerivesInitialIPv6OutboundFromNodeListener(t *testing.T) {
	config := domain.DefaultConfig()
	config.Nodes = []domain.Node{{
		ID:       "derived",
		Protocol: domain.ProtocolVLESS,
		Port:     24443,
		Enabled:  true,
		Credential: domain.NodeCredential{
			UUID: "123e4567-e89b-42d3-a456-426614174000",
		},
	}}
	payload, err := CompileServerConfig(Input{
		Config: config,
		Deployments: []NodeDeployment{{
			NodeID:    "derived",
			Listeners: []netip.Addr{netip.MustParseAddr("2001:db8:2::10")},
		}},
	})
	if err != nil {
		t.Fatalf("CompileServerConfig() error = %v", err)
	}
	text := string(payload)
	for _, expected := range []string{
		`"inet6_bind_address": "2001:db8:2::10"`,
		`"outbound": "direct-v6-1"`,
		`"outbound": "direct-v4"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("derived runtime config missing %s: %s", expected, text)
		}
	}
}

func TestCompileServerConfigRejectsEnabledNodesWithoutIPv6OutboundCandidates(t *testing.T) {
	config := domain.DefaultConfig()
	config.Nodes = []domain.Node{{
		ID:       "ipv4-only",
		Protocol: domain.ProtocolVLESS,
		Port:     24443,
		Enabled:  true,
		Credential: domain.NodeCredential{
			UUID: "123e4567-e89b-42d3-a456-426614174000",
		},
	}}
	_, err := CompileServerConfig(Input{
		Config: config,
		Deployments: []NodeDeployment{{
			NodeID:    "ipv4-only",
			Listeners: []netip.Addr{netip.MustParseAddr("198.51.100.10")},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "outbound") {
		t.Fatalf("missing IPv6 outbound error = %v", err)
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
