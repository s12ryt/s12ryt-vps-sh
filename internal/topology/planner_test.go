package topology

import (
	"reflect"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

func TestPlanClientIPv4ModeKeepsIPv4LocalAndAssignsIndependentOutbounds(t *testing.T) {
	config := topologyConfig(
		domain.RoutingModeClientIPv4,
		domain.TopologyMultiIPv6MultiNode,
		[]domain.Node{topologyNode("node-a", true), topologyNode("node-b", true)},
	)

	plan, err := BuildPlan(Input{
		Config:             config,
		OutboundCandidates: []OutboundCandidate{
			{Tag: "direct-v6-a", Kind: CandidateDirectIPv6},
			{Tag: "remote-vless-b", Kind: CandidateRemoteProxy},
		},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}

	if plan.IPv4.Action != IPv4ClientDirect || len(plan.IPv4.Candidates) != 0 {
		t.Fatalf("IPv4 policy = %#v", plan.IPv4)
	}
	wantRules := []ClientRule{
		{Family: AddressFamilyIPv4, Action: ClientActionDirect},
		{Family: AddressFamilyIPv6, Action: ClientActionProxy},
	}
	if !reflect.DeepEqual(plan.ClientRules, wantRules) {
		t.Fatalf("client rules = %#v, want %#v", plan.ClientRules, wantRules)
	}
	wantNodes := []NodePlan{
		{NodeID: "node-a", StaticOutbound: "direct-v6-a"},
		{NodeID: "node-b", StaticOutbound: "remote-vless-b"},
	}
	if !reflect.DeepEqual(plan.Nodes, wantNodes) {
		t.Fatalf("node plans = %#v, want %#v", plan.Nodes, wantNodes)
	}
}

func TestPlanVPSIPv4ModePreservesOrderedFallbacks(t *testing.T) {
	config := topologyConfig(
		domain.RoutingModeVPSIPv4,
		domain.TopologySingleIPv6SingleNode,
		[]domain.Node{topologyNode("single", true)},
	)
	plan, err := BuildPlan(Input{
		Config:             config,
		OutboundCandidates: []OutboundCandidate{{Tag: "direct-v6", Kind: CandidateDirectIPv6}},
		IPv4Candidates:     []string{"remote-socks", "direct-v4"},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}

	if plan.IPv4.Action != IPv4VPSFallback {
		t.Fatalf("IPv4 action = %q", plan.IPv4.Action)
	}
	if want := []string{"remote-socks", "direct-v4"}; !reflect.DeepEqual(plan.IPv4.Candidates, want) {
		t.Fatalf("IPv4 candidates = %#v, want %#v", plan.IPv4.Candidates, want)
	}
	if len(plan.ClientRules) != 0 {
		t.Fatalf("VPS mode unexpectedly emitted client rules: %#v", plan.ClientRules)
	}
}

func TestPlanIPv6OnlyModeRejectsIPv4Destinations(t *testing.T) {
	config := topologyConfig(
		domain.RoutingModeIPv6Only,
		domain.TopologySingleIPv6SingleNode,
		[]domain.Node{topologyNode("single", true)},
	)
	plan, err := BuildPlan(Input{
		Config:             config,
		OutboundCandidates: []OutboundCandidate{{Tag: "direct-v6", Kind: CandidateDirectIPv6}},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if plan.IPv4.Action != IPv4Reject || len(plan.IPv4.Candidates) != 0 {
		t.Fatalf("IPv4 policy = %#v", plan.IPv4)
	}
}

func TestPlanSingleRotatingNodeUsesSharedPoolForNewConnections(t *testing.T) {
	config := topologyConfig(
		domain.RoutingModeClientIPv4,
		domain.TopologyMultiIPv6RotatingNode,
		[]domain.Node{topologyNode("rotating", true)},
	)
	candidates := []OutboundCandidate{
		{Tag: "direct-v6-a", Kind: CandidateDirectIPv6},
		{Tag: "remote-tuic", Kind: CandidateRemoteProxy},
	}
	plan, err := BuildPlan(Input{Config: config, OutboundCandidates: candidates})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}

	if len(plan.Nodes) != 1 || plan.Nodes[0].Rotation == nil {
		t.Fatalf("node plans = %#v", plan.Nodes)
	}
	rotation := plan.Nodes[0].Rotation
	if !reflect.DeepEqual(rotation.Candidates, candidates) {
		t.Fatalf("rotation candidates = %#v, want %#v", rotation.Candidates, candidates)
	}
	if rotation.StartIndex != 0 || rotation.Interval != time.Hour || !rotation.NewConnectionsOnly {
		t.Fatalf("rotation = %#v", rotation)
	}
}

func TestPlanMultipleRotatingNodesStaggersSharedPool(t *testing.T) {
	config := topologyConfig(
		domain.RoutingModeClientIPv4,
		domain.TopologyMultiIPv6RotatingNodes,
		[]domain.Node{
			topologyNode("node-a", true),
			topologyNode("disabled", false),
			topologyNode("node-b", true),
			topologyNode("node-c", true),
		},
	)
	candidates := []OutboundCandidate{
		{Tag: "direct-v6-a", Kind: CandidateDirectIPv6},
		{Tag: "remote-anytls", Kind: CandidateRemoteProxy},
	}
	plan, err := BuildPlan(Input{Config: config, OutboundCandidates: candidates})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}

	wantIDs := []string{"node-a", "node-b", "node-c"}
	wantStartIndices := []int{0, 1, 0}
	if len(plan.Nodes) != len(wantIDs) {
		t.Fatalf("node plan count = %d, want %d", len(plan.Nodes), len(wantIDs))
	}
	for index, node := range plan.Nodes {
		if node.NodeID != wantIDs[index] || node.Rotation == nil {
			t.Fatalf("node plan %d = %#v", index, node)
		}
		if node.Rotation.StartIndex != wantStartIndices[index] {
			t.Fatalf("node %q start index = %d, want %d", node.NodeID, node.Rotation.StartIndex, wantStartIndices[index])
		}
		if !reflect.DeepEqual(node.Rotation.Candidates, candidates) || !node.Rotation.NewConnectionsOnly {
			t.Fatalf("node %q rotation = %#v", node.NodeID, node.Rotation)
		}
	}
}

func TestPlanRejectsContradictoryOrUnsafeTopologyInputs(t *testing.T) {
	validNode := topologyNode("node-a", true)
	twoNodes := []domain.Node{validNode, topologyNode("node-b", true)}
	direct := OutboundCandidate{Tag: "direct-v6", Kind: CandidateDirectIPv6}
	remote := OutboundCandidate{Tag: "remote-vless", Kind: CandidateRemoteProxy}

	tests := []struct {
		name  string
		input Input
	}{
		{
			name: "multi node requires one independent outbound per node",
			input: Input{
				Config:             topologyConfig(domain.RoutingModeClientIPv4, domain.TopologyMultiIPv6MultiNode, twoNodes),
				OutboundCandidates: []OutboundCandidate{direct},
			},
		},
		{
			name: "single topology requires one enabled node",
			input: Input{
				Config:             topologyConfig(domain.RoutingModeClientIPv4, domain.TopologySingleIPv6SingleNode, twoNodes),
				OutboundCandidates: []OutboundCandidate{direct},
			},
		},
		{
			name: "single topology requires one outbound",
			input: Input{
				Config:             topologyConfig(domain.RoutingModeClientIPv4, domain.TopologySingleIPv6SingleNode, []domain.Node{validNode}),
				OutboundCandidates: []OutboundCandidate{direct, remote},
			},
		},
		{
			name: "single rotating topology requires two candidates",
			input: Input{
				Config:             topologyConfig(domain.RoutingModeClientIPv4, domain.TopologyMultiIPv6RotatingNode, []domain.Node{validNode}),
				OutboundCandidates: []OutboundCandidate{direct},
			},
		},
		{
			name: "multiple rotating topology requires multiple nodes",
			input: Input{
				Config:             topologyConfig(domain.RoutingModeClientIPv4, domain.TopologyMultiIPv6RotatingNodes, []domain.Node{validNode}),
				OutboundCandidates: []OutboundCandidate{direct, remote},
			},
		},
		{
			name: "duplicate outbound tag",
			input: Input{
				Config:             topologyConfig(domain.RoutingModeClientIPv4, domain.TopologyMultiIPv6RotatingNode, []domain.Node{validNode}),
				OutboundCandidates: []OutboundCandidate{
					{Tag: "same", Kind: CandidateDirectIPv6},
					{Tag: "same", Kind: CandidateRemoteProxy},
				},
			},
		},
		{
			name: "unsafe outbound tag",
			input: Input{
				Config:             topologyConfig(domain.RoutingModeClientIPv4, domain.TopologyMultiIPv6RotatingNode, []domain.Node{validNode}),
				OutboundCandidates: []OutboundCandidate{
					{Tag: "../escape", Kind: CandidateDirectIPv6},
					remote,
				},
			},
		},
		{
			name: "unknown outbound kind",
			input: Input{
				Config:             topologyConfig(domain.RoutingModeClientIPv4, domain.TopologyMultiIPv6RotatingNode, []domain.Node{validNode}),
				OutboundCandidates: []OutboundCandidate{
					{Tag: "candidate-a", Kind: "unknown"},
					remote,
				},
			},
		},
		{
			name: "VPS mode requires ordered IPv4 candidates",
			input: Input{
				Config:             topologyConfig(domain.RoutingModeVPSIPv4, domain.TopologySingleIPv6SingleNode, []domain.Node{validNode}),
				OutboundCandidates: []OutboundCandidate{direct},
			},
		},
		{
			name: "duplicate IPv4 candidate",
			input: Input{
				Config:             topologyConfig(domain.RoutingModeVPSIPv4, domain.TopologySingleIPv6SingleNode, []domain.Node{validNode}),
				OutboundCandidates: []OutboundCandidate{direct},
				IPv4Candidates:     []string{"direct-v4", "direct-v4"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := BuildPlan(test.input); err == nil {
				t.Fatal("BuildPlan() accepted contradictory or unsafe input")
			}
		})
	}
}

func topologyConfig(mode domain.RoutingMode, topology domain.Topology, nodes []domain.Node) domain.Config {
	config := domain.DefaultConfig()
	config.Routing.Mode = mode
	config.Routing.Topology = topology
	config.Nodes = nodes
	return config
}

func topologyNode(id string, enabled bool) domain.Node {
	return domain.Node{
		ID:       id,
		Protocol: domain.ProtocolVLESS,
		Port:     24443,
		Enabled:  enabled,
		Credential: domain.NodeCredential{
			UUID: "550e8400-e29b-41d4-a716-446655440000",
		},
	}
}
