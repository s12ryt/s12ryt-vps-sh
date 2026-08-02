package singbox

import (
	"encoding/json"
	"net/netip"
	"reflect"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

func TestGenerateServerConfigSupportsAllRequiredInboundProtocols(t *testing.T) {
	protocols := []domain.Protocol{
		domain.ProtocolVLESS,
		domain.ProtocolVMess,
		domain.ProtocolHysteria2,
		domain.ProtocolTUIC,
		domain.ProtocolSOCKS5,
		domain.ProtocolAnyTLS,
		domain.ProtocolShadowsocks,
	}

	for index, protocol := range protocols {
		t.Run(string(protocol), func(t *testing.T) {
			input := ServerInput{
				Nodes: []InboundNode{{
					ID:        "node-1",
					Protocol:  protocol,
					Port:      22000 + index,
					Listeners: []netip.Addr{netip.MustParseAddr("2001:db8:abcd::10")},
					Credential: Credential{
						Username: "user-1",
						UUID:     "550e8400-e29b-41d4-a716-446655440000",
						Password: "strong-password",
						Method:   "2022-blake3-aes-128-gcm",
					},
					TLS: TLSConfig{Enabled: protocolUsesTLS(protocol), CertificatePath: "/opt/s12ryt-ipv6/tls/cert.pem", KeyPath: "/opt/s12ryt-ipv6/tls/key.pem"},
				}},
				IPv6Outbounds: []netip.Addr{netip.MustParseAddr("2001:db8:abcd::20")},
			}

			payload, err := GenerateServerConfig(input)
			if err != nil {
				t.Fatalf("GenerateServerConfig() error = %v", err)
			}
			config := decodeConfig(t, payload)
			inbounds := config["inbounds"].([]any)
			if len(inbounds) != 1 {
				t.Fatalf("inbound count = %d, want 1", len(inbounds))
			}
			inbound := inbounds[0].(map[string]any)
			wantType := string(protocol)
			if protocol == domain.ProtocolSOCKS5 {
				wantType = "socks"
			}
			if inbound["type"] != wantType || inbound["tag"] != "in-node-1-v6" {
				t.Fatalf("inbound identity = %#v", inbound)
			}
			if inbound["listen"] != "2001:db8:abcd::10" || inbound["listen_port"] != float64(22000+index) {
				t.Fatalf("inbound listener = %#v", inbound)
			}
			assertProtocolCredential(t, protocol, inbound)
		})
	}
}

func TestGenerateServerConfigCreatesDualListenersWithSharedPortAndCredential(t *testing.T) {
	input := ServerInput{
		Nodes: []InboundNode{{
			ID:        "dual-node",
			Protocol:  domain.ProtocolVLESS,
			Port:      24444,
			Listeners: []netip.Addr{netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("2001:db8::10")},
			Credential: Credential{UUID: "550e8400-e29b-41d4-a716-446655440000"},
		}},
	}
	payload, err := GenerateServerConfig(input)
	if err != nil {
		t.Fatalf("GenerateServerConfig() error = %v", err)
	}
	config := decodeConfig(t, payload)
	inbounds := config["inbounds"].([]any)
	if len(inbounds) != 2 {
		t.Fatalf("inbound count = %d, want 2", len(inbounds))
	}
	first := inbounds[0].(map[string]any)
	second := inbounds[1].(map[string]any)
	if first["tag"] != "in-dual-node-v4" || second["tag"] != "in-dual-node-v6" {
		t.Fatalf("listener tags = %q, %q", first["tag"], second["tag"])
	}
	if first["listen_port"] != second["listen_port"] || !reflect.DeepEqual(first["users"], second["users"]) {
		t.Fatal("dual listeners do not share a port and credential")
	}
}

func TestGenerateServerConfigBindsDirectOutboundsToExactIPv6Addresses(t *testing.T) {
	input := ServerInput{IPv6Outbounds: []netip.Addr{
		netip.MustParseAddr("2001:db8:abcd::20"),
		netip.MustParseAddr("2001:db8:abcd::21"),
	}}
	payload, err := GenerateServerConfig(input)
	if err != nil {
		t.Fatalf("GenerateServerConfig() error = %v", err)
	}
	config := decodeConfig(t, payload)
	outbounds := config["outbounds"].([]any)
	want := []map[string]any{
		{"type": "direct", "tag": "direct-v6-1", "inet6_bind_address": "2001:db8:abcd::20"},
		{"type": "direct", "tag": "direct-v6-2", "inet6_bind_address": "2001:db8:abcd::21"},
	}
	for index := range want {
		if !reflect.DeepEqual(outbounds[index], want[index]) {
			t.Fatalf("outbound %d = %#v, want %#v", index, outbounds[index], want[index])
		}
	}
}

func TestGenerateServerConfigRejectsUnsafeNodeInputs(t *testing.T) {
	base := InboundNode{
		ID:        "node-1",
		Protocol:  domain.ProtocolVLESS,
		Port:      22000,
		Listeners: []netip.Addr{netip.MustParseAddr("2001:db8::10")},
		Credential: Credential{UUID: "550e8400-e29b-41d4-a716-446655440000"},
	}
	tests := []struct {
		name   string
		mutate func(*InboundNode)
	}{
		{name: "missing listener", mutate: func(node *InboundNode) { node.Listeners = nil }},
		{name: "unspecified listener", mutate: func(node *InboundNode) { node.Listeners = []netip.Addr{netip.IPv6Unspecified()} }},
		{name: "port outside project range", mutate: func(node *InboundNode) { node.Port = 19999 }},
		{name: "unsupported protocol", mutate: func(node *InboundNode) { node.Protocol = domain.Protocol("unknown") }},
		{name: "missing credential", mutate: func(node *InboundNode) { node.Credential = Credential{} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := base
			test.mutate(&node)
			if _, err := GenerateServerConfig(ServerInput{Nodes: []InboundNode{node}}); err == nil {
				t.Fatal("GenerateServerConfig() accepted unsafe node input")
			}
		})
	}

	duplicate := ServerInput{Nodes: []InboundNode{base, base}}
	if _, err := GenerateServerConfig(duplicate); err == nil {
		t.Fatal("GenerateServerConfig() accepted duplicate node IDs")
	}
}

func decodeConfig(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var config map[string]any
	if err := json.Unmarshal(payload, &config); err != nil {
		t.Fatalf("generated payload is not JSON: %v", err)
	}
	return config
}

func protocolUsesTLS(protocol domain.Protocol) bool {
	return protocol == domain.ProtocolHysteria2 || protocol == domain.ProtocolTUIC || protocol == domain.ProtocolAnyTLS
}

func assertProtocolCredential(t *testing.T, protocol domain.Protocol, inbound map[string]any) {
	t.Helper()
	if protocol == domain.ProtocolShadowsocks {
		if inbound["method"] != "2022-blake3-aes-128-gcm" || inbound["password"] != "strong-password" {
			t.Fatalf("Shadowsocks credential = %#v", inbound)
		}
		return
	}
	users, ok := inbound["users"].([]any)
	if !ok || len(users) != 1 {
		t.Fatalf("users = %#v", inbound["users"])
	}
	user := users[0].(map[string]any)
	switch protocol {
	case domain.ProtocolVLESS, domain.ProtocolVMess:
		if user["uuid"] != "550e8400-e29b-41d4-a716-446655440000" {
			t.Fatalf("UUID credential = %#v", user)
		}
	case domain.ProtocolTUIC:
		if user["uuid"] == nil || user["password"] != "strong-password" {
			t.Fatalf("TUIC credential = %#v", user)
		}
	default:
		if user["password"] != "strong-password" {
			t.Fatalf("password credential = %#v", user)
		}
	}
}
