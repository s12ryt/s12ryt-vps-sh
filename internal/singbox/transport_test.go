package singbox

import (
	"net/netip"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

func TestGenerateServerConfigBuildsVLESSAndVMessTransports(t *testing.T) {
	tests := []struct {
		name      string
		protocol  domain.Protocol
		transport TransportConfig
		wantType  string
		wantKey   string
		wantValue string
	}{
		{name: "VLESS TCP TLS", protocol: domain.ProtocolVLESS},
		{name: "VLESS WebSocket TLS", protocol: domain.ProtocolVLESS, transport: TransportConfig{Type: TransportWebSocket, Path: "/edge"}, wantType: "ws", wantKey: "path", wantValue: "/edge"},
		{name: "VLESS gRPC TLS", protocol: domain.ProtocolVLESS, transport: TransportConfig{Type: TransportGRPC, ServiceName: "edgeService"}, wantType: "grpc", wantKey: "service_name", wantValue: "edgeService"},
		{name: "VMess TCP TLS", protocol: domain.ProtocolVMess},
		{name: "VMess WebSocket TLS", protocol: domain.ProtocolVMess, transport: TransportConfig{Type: TransportWebSocket, Path: "/vmess"}, wantType: "ws", wantKey: "path", wantValue: "/vmess"},
		{name: "VMess gRPC TLS", protocol: domain.ProtocolVMess, transport: TransportConfig{Type: TransportGRPC, ServiceName: "vmessService"}, wantType: "grpc", wantKey: "service_name", wantValue: "vmessService"},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, err := GenerateServerConfig(ServerInput{Nodes: []InboundNode{{
				ID:         "transport-node",
				Protocol:   test.protocol,
				Port:       25000 + index,
				Listeners:  []netip.Addr{netip.MustParseAddr("2001:db8:abcd::10")},
				Credential: Credential{UUID: "550e8400-e29b-41d4-a716-446655440000"},
				TLS:        TLSConfig{Enabled: true, ServerName: "node.example.com", CertificatePath: "/opt/s12ryt-ipv6/tls/cert.pem", KeyPath: "/opt/s12ryt-ipv6/tls/key.pem"},
				Transport:  test.transport,
			}}})
			if err != nil {
				t.Fatalf("GenerateServerConfig() error = %v", err)
			}
			inbound := decodeConfig(t, payload)["inbounds"].([]any)[0].(map[string]any)
			tls := inbound["tls"].(map[string]any)
			if tls["server_name"] != "node.example.com" || tls["certificate_path"] == nil || tls["key_path"] == nil {
				t.Fatalf("TLS config = %#v", tls)
			}
			if test.wantType == "" {
				if _, exists := inbound["transport"]; exists {
					t.Fatalf("plain TCP unexpectedly has transport: %#v", inbound["transport"])
				}
				return
			}
			transport := inbound["transport"].(map[string]any)
			if transport["type"] != test.wantType || transport[test.wantKey] != test.wantValue {
				t.Fatalf("transport = %#v", transport)
			}
		})
	}
}

func TestGenerateServerConfigBuildsVLESSReality(t *testing.T) {
	payload, err := GenerateServerConfig(ServerInput{Nodes: []InboundNode{{
		ID:         "reality-node",
		Protocol:   domain.ProtocolVLESS,
		Port:       26001,
		Listeners:  []netip.Addr{netip.MustParseAddr("2001:db8:abcd::10")},
		Credential: Credential{UUID: "550e8400-e29b-41d4-a716-446655440000"},
		TLS: TLSConfig{
			Enabled:    true,
			ServerName: "www.example.com",
			Reality: &RealityConfig{
				HandshakeServer: "www.example.com",
				HandshakePort:   443,
				PrivateKey:      "private-key",
				ShortID:         "0123456789abcdef",
			},
		},
	}}})
	if err != nil {
		t.Fatalf("GenerateServerConfig() error = %v", err)
	}
	inbound := decodeConfig(t, payload)["inbounds"].([]any)[0].(map[string]any)
	tls := inbound["tls"].(map[string]any)
	reality := tls["reality"].(map[string]any)
	handshake := reality["handshake"].(map[string]any)
	if reality["enabled"] != true || reality["private_key"] != "private-key" {
		t.Fatalf("Reality config = %#v", reality)
	}
	shortIDs := reality["short_id"].([]any)
	if len(shortIDs) != 1 || shortIDs[0] != "0123456789abcdef" {
		t.Fatalf("Reality short IDs = %#v", shortIDs)
	}
	if handshake["server"] != "www.example.com" || handshake["server_port"] != float64(443) {
		t.Fatalf("Reality handshake = %#v", handshake)
	}
}

func TestGenerateServerConfigBuildsACMEHTTP01TLS(t *testing.T) {
	payload, err := GenerateServerConfig(ServerInput{Nodes: []InboundNode{{
		ID:         "acme-node",
		Protocol:   domain.ProtocolVLESS,
		Port:       26003,
		Listeners:  []netip.Addr{netip.MustParseAddr("2001:db8:abcd::10")},
		Credential: Credential{UUID: "550e8400-e29b-41d4-a716-446655440000"},
		TLS: TLSConfig{
			Enabled:    true,
			ServerName: "node.example.com",
			ACME: &ACMEConfig{
				Domains:           []string{"node.example.com"},
				DataDirectory:     "/opt/s12ryt-ipv6/tls/acme",
				DefaultServerName: "node.example.com",
				Email:             "admin@example.com",
				Provider:          "letsencrypt",
			},
		},
	}}})
	if err != nil {
		t.Fatalf("GenerateServerConfig() error = %v", err)
	}
	inbound := decodeConfig(t, payload)["inbounds"].([]any)[0].(map[string]any)
	tls := inbound["tls"].(map[string]any)
	if _, exists := tls["certificate_path"]; exists {
		t.Fatalf("ACME TLS unexpectedly contains certificate_path: %#v", tls)
	}
	if _, exists := tls["key_path"]; exists {
		t.Fatalf("ACME TLS unexpectedly contains key_path: %#v", tls)
	}
	acme := tls["acme"].(map[string]any)
	domains := acme["domain"].([]any)
	if len(domains) != 1 || domains[0] != "node.example.com" {
		t.Fatalf("ACME domains = %#v", domains)
	}
	if acme["data_directory"] != "/opt/s12ryt-ipv6/tls/acme" || acme["default_server_name"] != "node.example.com" {
		t.Fatalf("ACME storage or default server = %#v", acme)
	}
	if acme["email"] != "admin@example.com" || acme["provider"] != "letsencrypt" {
		t.Fatalf("ACME account = %#v", acme)
	}
	if acme["disable_http_challenge"] != false || acme["disable_tls_alpn_challenge"] != true {
		t.Fatalf("ACME challenge policy = %#v", acme)
	}
}

func TestGenerateServerConfigRejectsUnsafeTransportAndReality(t *testing.T) {
	base := InboundNode{
		ID:         "transport-node",
		Protocol:   domain.ProtocolVLESS,
		Port:       26002,
		Listeners:  []netip.Addr{netip.MustParseAddr("2001:db8:abcd::10")},
		Credential: Credential{UUID: "550e8400-e29b-41d4-a716-446655440000"},
		TLS:        TLSConfig{Enabled: true, CertificatePath: "/cert.pem", KeyPath: "/key.pem"},
	}
	tests := []struct {
		name   string
		mutate func(*InboundNode)
	}{
		{name: "unsupported transport", mutate: func(node *InboundNode) { node.Transport = TransportConfig{Type: "quic"} }},
		{name: "WebSocket missing path", mutate: func(node *InboundNode) { node.Transport = TransportConfig{Type: TransportWebSocket} }},
		{name: "gRPC unsafe service", mutate: func(node *InboundNode) {
			node.Transport = TransportConfig{Type: TransportGRPC, ServiceName: "../service"}
		}},
		{name: "transport on Hysteria2", mutate: func(node *InboundNode) {
			node.Protocol = domain.ProtocolHysteria2
			node.Credential = Credential{Password: "password"}
			node.Transport = TransportConfig{Type: TransportWebSocket, Path: "/edge"}
		}},
		{name: "Reality on VMess", mutate: func(node *InboundNode) {
			node.Protocol = domain.ProtocolVMess
			node.TLS = TLSConfig{Enabled: true, Reality: validRealityConfig()}
		}},
		{name: "Reality missing handshake", mutate: func(node *InboundNode) {
			node.TLS = TLSConfig{Enabled: true, Reality: &RealityConfig{PrivateKey: "key", ShortID: "0123456789abcdef"}}
		}},
		{name: "Reality invalid short ID", mutate: func(node *InboundNode) {
			reality := validRealityConfig()
			reality.ShortID = "not-hex"
			node.TLS = TLSConfig{Enabled: true, Reality: reality}
		}},
		{name: "ACME with certificate files", mutate: func(node *InboundNode) {
			node.TLS.ACME = validACMEConfig()
		}},
		{name: "ACME without domains", mutate: func(node *InboundNode) {
			node.TLS = TLSConfig{Enabled: true, ACME: &ACMEConfig{DataDirectory: "/opt/s12ryt-ipv6/tls/acme"}}
		}},
		{name: "ACME outside project directory", mutate: func(node *InboundNode) {
			acme := validACMEConfig()
			acme.DataDirectory = "/tmp/acme"
			node.TLS = TLSConfig{Enabled: true, ACME: acme}
		}},
		{name: "ACME with Reality", mutate: func(node *InboundNode) {
			node.TLS = TLSConfig{Enabled: true, ACME: validACMEConfig(), Reality: validRealityConfig()}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := base
			test.mutate(&node)
			if _, err := GenerateServerConfig(ServerInput{Nodes: []InboundNode{node}}); err == nil {
				t.Fatal("GenerateServerConfig() accepted unsafe transport or Reality input")
			}
		})
	}
}

func validACMEConfig() *ACMEConfig {
	return &ACMEConfig{
		Domains:           []string{"node.example.com"},
		DataDirectory:     "/opt/s12ryt-ipv6/tls/acme",
		DefaultServerName: "node.example.com",
		Email:             "admin@example.com",
		Provider:          "letsencrypt",
	}
}

func validRealityConfig() *RealityConfig {
	return &RealityConfig{
		HandshakeServer: "www.example.com",
		HandshakePort:   443,
		PrivateKey:      "private-key",
		ShortID:         "0123456789abcdef",
	}
}
