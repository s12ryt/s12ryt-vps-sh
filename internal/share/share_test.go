package share

import (
	"encoding/base64"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

func TestGenerateBundleBuildsVLESSURIClientJSONAndQRPayload(t *testing.T) {
	node := LocalNode{
		ID:       "edge-vless",
		Name:     "台北 edge",
		Protocol: domain.ProtocolVLESS,
		Server:   netip.MustParseAddr("2001:db8:abcd::10"),
		Port:     24443,
		UUID:     "550e8400-e29b-41d4-a716-446655440000",
		Enabled:  true,
		Healthy:  true,
		TLS:      TLSOptions{ServerName: "edge.example.com"},
		Transport: TransportOptions{
			Type: "ws",
			Path: "/edge path",
		},
	}

	bundle, err := GenerateBundle(Input{LocalNodes: []LocalNode{node}})
	if err != nil {
		t.Fatalf("GenerateBundle() error = %v", err)
	}
	if len(bundle.Nodes) != 1 {
		t.Fatalf("node artifacts = %d, want 1", len(bundle.Nodes))
	}
	artifact := bundle.Nodes[0]
	for _, fragment := range []string{
		"vless://550e8400-e29b-41d4-a716-446655440000@[2001:db8:abcd::10]:24443?",
		"security=tls",
		"sni=edge.example.com",
		"type=ws",
		"path=%2Fedge+path",
		"#%E5%8F%B0%E5%8C%97+edge",
	} {
		if !strings.Contains(artifact.URI, fragment) {
			t.Fatalf("URI %q does not contain %q", artifact.URI, fragment)
		}
	}
	if artifact.QRPayload != artifact.URI {
		t.Fatal("QR payload must be the exact node URI")
	}
	var client map[string]any
	if err := json.Unmarshal(artifact.ClientJSON, &client); err != nil {
		t.Fatalf("client JSON is invalid: %v", err)
	}
	outbound := client["outbounds"].([]any)[0].(map[string]any)
	if outbound["type"] != "vless" || outbound["server"] != "2001:db8:abcd::10" || outbound["server_port"] != float64(24443) {
		t.Fatalf("client outbound = %#v", outbound)
	}
}

func TestGenerateBundleSupportsEveryRequiredProtocolURI(t *testing.T) {
	protocols := []struct {
		protocol domain.Protocol
		prefix   string
	}{
		{domain.ProtocolVLESS, "vless://"},
		{domain.ProtocolVMess, "vmess://"},
		{domain.ProtocolHysteria2, "hysteria2://"},
		{domain.ProtocolTUIC, "tuic://"},
		{domain.ProtocolSOCKS5, "socks5://"},
		{domain.ProtocolAnyTLS, "anytls://"},
		{domain.ProtocolShadowsocks, "ss://"},
	}
	for index, test := range protocols {
		t.Run(string(test.protocol), func(t *testing.T) {
			node := completeNode(test.protocol, 24000+index)
			bundle, err := GenerateBundle(Input{LocalNodes: []LocalNode{node}})
			if err != nil {
				t.Fatalf("GenerateBundle() error = %v", err)
			}
			if !strings.HasPrefix(bundle.Nodes[0].URI, test.prefix) {
				t.Fatalf("URI = %q, want prefix %q", bundle.Nodes[0].URI, test.prefix)
			}
			if len(bundle.Nodes[0].ClientJSON) == 0 || bundle.Nodes[0].QRPayload == "" {
				t.Fatal("share artifact is incomplete")
			}
		})
	}
}

func TestAggregateSubscriptionContainsOnlyEnabledHealthyLocalNodes(t *testing.T) {
	enabled := completeNode(domain.ProtocolVLESS, 25001)
	enabled.ID = "enabled"
	disabled := completeNode(domain.ProtocolVMess, 25002)
	disabled.ID = "disabled"
	disabled.Enabled = false
	unhealthy := completeNode(domain.ProtocolTUIC, 25003)
	unhealthy.ID = "unhealthy"
	unhealthy.Healthy = false

	bundle, err := GenerateBundle(Input{
		LocalNodes:    []LocalNode{enabled, disabled, unhealthy},
		RemoteSecrets: []string{"remote-password-must-not-leak", "socks5://remote-secret@example.com:1080"},
	})
	if err != nil {
		t.Fatalf("GenerateBundle() error = %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(bundle.Subscription)
	if err != nil {
		t.Fatalf("subscription is not standard Base64: %v", err)
	}
	text := string(decoded)
	if !strings.Contains(text, bundle.Nodes[0].URI) {
		t.Fatalf("subscription does not contain enabled local node: %q", text)
	}
	for _, forbidden := range []string{"disabled", "unhealthy", "remote-password-must-not-leak", "remote-secret"} {
		if strings.Contains(text, forbidden) || strings.Contains(string(bundle.Nodes[0].ClientJSON), forbidden) {
			t.Fatalf("share output leaked excluded data %q", forbidden)
		}
	}
}

func TestClientIPv4ModeBuildsCompleteSplitRoutingConfiguration(t *testing.T) {
	node := completeNode(domain.ProtocolVLESS, 25555)
	input := Input{
		LocalNodes:  []LocalNode{node},
		RoutingMode: domain.RoutingModeClientIPv4,
	}

	bundle, err := GenerateBundle(input)
	if err != nil {
		t.Fatalf("GenerateBundle() error = %v", err)
	}
	artifact := bundle.Nodes[0]
	if len(artifact.FullClientJSON) == 0 {
		t.Fatal("mode 1 full client JSON is empty")
	}
	if artifact.FullClientBase64 == "" {
		t.Fatal("mode 1 full client Base64 is empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(artifact.FullClientBase64)
	if err != nil {
		t.Fatalf("full client configuration is not standard Base64: %v", err)
	}
	if string(decoded) != string(artifact.FullClientJSON) {
		t.Fatal("full client Base64 does not decode to the JSON payload")
	}

	var config map[string]any
	if err := json.Unmarshal(artifact.FullClientJSON, &config); err != nil {
		t.Fatalf("full client JSON is invalid: %v", err)
	}
	inbounds := config["inbounds"].([]any)
	tun := inbounds[0].(map[string]any)
	if tun["type"] != "tun" || tun["tag"] != "tun-in" || tun["auto_route"] != true || tun["strict_route"] != true {
		t.Fatalf("TUN inbound = %#v", tun)
	}
	outbounds := config["outbounds"].([]any)
	if outbounds[0].(map[string]any)["tag"] != "proxy" || outbounds[1].(map[string]any)["tag"] != "direct" {
		t.Fatalf("full client outbounds = %#v", outbounds)
	}
	route := config["route"].(map[string]any)
	rules := route["rules"].([]any)
	if len(rules) != 2 {
		t.Fatalf("route rules = %#v, want two family rules", rules)
	}
	if rules[0].(map[string]any)["ip_version"] != float64(4) || rules[0].(map[string]any)["outbound"] != "direct" {
		t.Fatalf("IPv4 split rule = %#v", rules[0])
	}
	if rules[1].(map[string]any)["ip_version"] != float64(6) || rules[1].(map[string]any)["outbound"] != "proxy" {
		t.Fatalf("IPv6 split rule = %#v", rules[1])
	}
	if !strings.Contains(artifact.SplitRoutingWarning, "URI") || !strings.Contains(artifact.SplitRoutingWarning, "QR") ||
		!strings.Contains(artifact.SplitRoutingWarning, "分流規則") {
		t.Fatalf("split routing warning = %q", artifact.SplitRoutingWarning)
	}
}

func TestOtherModesDoNotAdvertiseClientSideSplitRouting(t *testing.T) {
	for _, mode := range []domain.RoutingMode{"", domain.RoutingModeVPSIPv4, domain.RoutingModeIPv6Only} {
		t.Run(string(mode), func(t *testing.T) {
			bundle, err := GenerateBundle(Input{
				LocalNodes:  []LocalNode{completeNode(domain.ProtocolVLESS, 25556)},
				RoutingMode: mode,
			})
			if err != nil {
				t.Fatalf("GenerateBundle() error = %v", err)
			}
			artifact := bundle.Nodes[0]
			if len(artifact.FullClientJSON) != 0 || artifact.FullClientBase64 != "" || artifact.SplitRoutingWarning != "" {
				t.Fatalf("mode %q unexpectedly produced client split routing output: %#v", mode, artifact)
			}
		})
	}
}

func TestGenerateBundleRejectsUnsafeOrIncompleteLocalNodes(t *testing.T) {
	base := completeNode(domain.ProtocolVLESS, 26001)
	tests := []struct {
		name   string
		mutate func(*LocalNode)
	}{
		{name: "unsafe ID", mutate: func(node *LocalNode) { node.ID = "../node" }},
		{name: "missing server", mutate: func(node *LocalNode) { node.Server = netip.Addr{} }},
		{name: "privileged port", mutate: func(node *LocalNode) { node.Port = 443 }},
		{name: "missing UUID", mutate: func(node *LocalNode) { node.UUID = "" }},
		{name: "unsupported protocol", mutate: func(node *LocalNode) { node.Protocol = domain.Protocol("http") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := base
			test.mutate(&node)
			if _, err := GenerateBundle(Input{LocalNodes: []LocalNode{node}}); err == nil {
				t.Fatal("GenerateBundle() accepted unsafe node")
			}
		})
	}
}

func completeNode(protocol domain.Protocol, port int) LocalNode {
	return LocalNode{
		ID:       "node-1",
		Name:     "node-1",
		Protocol: protocol,
		Server:   netip.MustParseAddr("2001:db8:abcd::10"),
		Port:     port,
		Username: "user",
		UUID:     "550e8400-e29b-41d4-a716-446655440000",
		Password: "strong-password",
		Method:   "2022-blake3-aes-128-gcm",
		Enabled:  true,
		Healthy:  true,
		TLS:      TLSOptions{ServerName: "node.example.com"},
	}
}
