package resourceprofile

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/runtimeconfig"
)

func TestBuildCreatesContractResourceWorkload(t *testing.T) {
	entropy := make([]byte, ResourceNodeCount*16)
	for index := range entropy {
		entropy[index] = byte(index)
	}

	profile, err := Build(bytes.NewReader(entropy))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if profile.Config.IPv6.GeneratedCount != ResourceIPv6Count {
		t.Fatalf("GeneratedCount = %d, want %d", profile.Config.IPv6.GeneratedCount, ResourceIPv6Count)
	}
	if profile.Config.Routing.Mode != domain.RoutingModeVPSIPv4 ||
		profile.Config.Routing.Topology != domain.TopologyMultiIPv6MultiNode {
		t.Fatalf("routing = %#v, want VPS IPv4 multi-node topology", profile.Config.Routing)
	}
	if len(profile.Config.Nodes) != ResourceNodeCount || len(profile.State.Nodes) != ResourceNodeCount {
		t.Fatalf("nodes = %d/%d, want %d", len(profile.Config.Nodes), len(profile.State.Nodes), ResourceNodeCount)
	}
	if len(profile.State.IPv6Outbounds) != ResourceIPv6Count || len(profile.Addresses) != ResourceIPv6Count {
		t.Fatalf("IPv6 addresses = %d/%d, want %d", len(profile.State.IPv6Outbounds), len(profile.Addresses), ResourceIPv6Count)
	}

	seenCredentials := make(map[string]struct{}, ResourceNodeCount)
	for index, node := range profile.Config.Nodes {
		if !node.Enabled || node.Protocol != domain.ProtocolVLESS {
			t.Fatalf("node %d = %#v, want enabled VLESS", index, node)
		}
		if node.Port != ResourceBasePort+index {
			t.Fatalf("node %d port = %d, want %d", index, node.Port, ResourceBasePort+index)
		}
		if _, duplicate := seenCredentials[node.Credential.UUID]; duplicate {
			t.Fatalf("node %d reused UUID %q", index, node.Credential.UUID)
		}
		seenCredentials[node.Credential.UUID] = struct{}{}

		deployment := profile.State.Nodes[index]
		if deployment.NodeID != node.ID || len(deployment.Listeners) != 1 || deployment.Listeners[0] != profile.Addresses[index] {
			t.Fatalf("deployment %d = %#v, want listener %s", index, deployment, profile.Addresses[index])
		}
	}

	input, err := profile.State.Resolve(profile.Config)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	payload, err := runtimeconfig.CompileServerConfig(input)
	if err != nil {
		t.Fatalf("CompileServerConfig() error = %v", err)
	}
	var compiled struct {
		Inbounds  []json.RawMessage `json:"inbounds"`
		Outbounds []json.RawMessage `json:"outbounds"`
	}
	if err := json.Unmarshal(payload, &compiled); err != nil {
		t.Fatalf("decode compiled config: %v", err)
	}
	if len(compiled.Inbounds) != ResourceNodeCount {
		t.Fatalf("compiled inbounds = %d, want %d", len(compiled.Inbounds), ResourceNodeCount)
	}
	if len(compiled.Outbounds) != ResourceIPv6Count+1 {
		t.Fatalf("compiled outbounds = %d, want %d direct IPv6 plus IPv4", len(compiled.Outbounds), ResourceIPv6Count+1)
	}
}

func TestBuildRejectsMissingEntropy(t *testing.T) {
	if _, err := Build(nil); err == nil {
		t.Fatal("Build() accepted missing entropy")
	}
}
