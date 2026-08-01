package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfigMatchesOperationalContract(t *testing.T) {
	config := DefaultConfig()

	if config.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", config.SchemaVersion)
	}
	if config.Panel.Port != 34456 {
		t.Fatalf("Panel.Port = %d, want 34456", config.Panel.Port)
	}
	if config.IPv6.GeneratedCount != 16 {
		t.Fatalf("IPv6.GeneratedCount = %d, want 16", config.IPv6.GeneratedCount)
	}
	if config.Routing.Mode != RoutingModeVPSIPv4 {
		t.Fatalf("Routing.Mode = %q, want %q", config.Routing.Mode, RoutingModeVPSIPv4)
	}
	if config.Routing.Topology != TopologyMultiIPv6MultiNode {
		t.Fatalf("Routing.Topology = %q, want %q", config.Routing.Topology, TopologyMultiIPv6MultiNode)
	}
	if config.Routing.RotationInterval != time.Hour {
		t.Fatalf("RotationInterval = %s, want 1h", config.Routing.RotationInterval)
	}
	if config.Health.URL != "https://cloudflare.com/cdn-cgi/trace" {
		t.Fatalf("Health.URL = %q", config.Health.URL)
	}
	if config.Health.Interval != 30*time.Second || config.Health.Timeout != 5*time.Second {
		t.Fatalf("health timing = %s/%s, want 30s/5s", config.Health.Interval, config.Health.Timeout)
	}
	if config.Health.FailureThreshold != 3 || config.Health.RecoveryThreshold != 3 {
		t.Fatalf("health thresholds = %d/%d, want 3/3", config.Health.FailureThreshold, config.Health.RecoveryThreshold)
	}
}

func TestGenerateBootstrapSecretsUsesRequiredAlphabetAndLengths(t *testing.T) {
	reader := strings.NewReader(strings.Repeat("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", 2))
	secrets, err := GenerateBootstrapSecrets(reader)
	if err != nil {
		t.Fatalf("GenerateBootstrapSecrets() error = %v", err)
	}

	if len(secrets.Password) != 24 {
		t.Fatalf("password length = %d, want 24", len(secrets.Password))
	}
	if len(secrets.WebPath) != 13 || secrets.WebPath[0] != '/' {
		t.Fatalf("web path = %q, want slash plus 12 characters", secrets.WebPath)
	}
	for _, value := range []string{secrets.Password, strings.TrimPrefix(secrets.WebPath, "/")} {
		for _, character := range value {
			if !strings.ContainsRune("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", character) {
				t.Fatalf("generated value %q contains invalid character %q", value, character)
			}
		}
	}
}

func TestGenerateBootstrapSecretsRejectsInsufficientEntropy(t *testing.T) {
	_, err := GenerateBootstrapSecrets(strings.NewReader("short"))
	if !errors.Is(err, ErrInsufficientEntropy) {
		t.Fatalf("error = %v, want ErrInsufficientEntropy", err)
	}
}

func TestConfigValidateAcceptsSupportedProtocolsModesAndTopologies(t *testing.T) {
	protocols := []Protocol{
		ProtocolVLESS,
		ProtocolVMess,
		ProtocolHysteria2,
		ProtocolTUIC,
		ProtocolSOCKS5,
		ProtocolAnyTLS,
		ProtocolShadowsocks,
	}
	modes := []RoutingMode{RoutingModeClientIPv4, RoutingModeVPSIPv4, RoutingModeIPv6Only}
	topologies := []Topology{
		TopologyMultiIPv6MultiNode,
		TopologySingleIPv6SingleNode,
		TopologyMultiIPv6RotatingNode,
		TopologyMultiIPv6RotatingNodes,
	}

	for _, protocol := range protocols {
		for _, mode := range modes {
			for _, topology := range topologies {
				config := DefaultConfig()
				config.Routing.Mode = mode
				config.Routing.Topology = topology
				config.Nodes = []Node{{ID: "node-1", Protocol: protocol, Port: 20000, Enabled: true}}
				if err := config.Validate(); err != nil {
					t.Fatalf("Validate(%q, %q, %q) error = %v", protocol, mode, topology, err)
				}
			}
		}
	}
}

func TestConfigValidateRejectsUnsafeBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "panel port below range", mutate: func(config *Config) { config.Panel.Port = 0 }},
		{name: "web path missing slash", mutate: func(config *Config) { config.Panel.Path = "admin" }},
		{name: "web path has separators", mutate: func(config *Config) { config.Panel.Path = "/admin/path" }},
		{name: "generated IPv6 count below range", mutate: func(config *Config) { config.IPv6.GeneratedCount = 0 }},
		{name: "generated IPv6 count above range", mutate: func(config *Config) { config.IPv6.GeneratedCount = 257 }},
		{name: "rotation interval not positive", mutate: func(config *Config) { config.Routing.RotationInterval = 0 }},
		{name: "health URL is not HTTPS", mutate: func(config *Config) { config.Health.URL = "http://example.com" }},
		{name: "node port below automatic range", mutate: func(config *Config) { config.Nodes = []Node{{ID: "node-1", Protocol: ProtocolVLESS, Port: 19999}} }},
		{name: "duplicate node ID", mutate: func(config *Config) {
			config.Nodes = []Node{
				{ID: "node-1", Protocol: ProtocolVLESS, Port: 20000},
				{ID: "node-1", Protocol: ProtocolVMess, Port: 20001},
			}
		}},
		{name: "unsupported protocol", mutate: func(config *Config) { config.Nodes = []Node{{ID: "node-1", Protocol: Protocol("unknown"), Port: 20000}} }},
		{name: "unsupported routing mode", mutate: func(config *Config) { config.Routing.Mode = RoutingMode("unknown") }},
		{name: "unsupported topology", mutate: func(config *Config) { config.Routing.Topology = Topology("unknown") }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultConfig()
			config.Panel.Path = "/abcdefghijkl"
			test.mutate(&config)
			if err := config.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want rejection")
			}
		})
	}
}
