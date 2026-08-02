package singbox

import (
	"encoding/json"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/topology"
)

func TestGenerateServerConfigRoutesNodeTrafficByAddressFamily(t *testing.T) {
	plan := topology.Plan{
		Mode:     domain.RoutingModeVPSIPv4,
		Topology: domain.TopologySingleIPv6SingleNode,
		IPv4: topology.IPv4Policy{
			Action:     topology.IPv4VPSFallback,
			Candidates: []string{"direct-v4"},
		},
		Nodes: []topology.NodePlan{{NodeID: "edge", StaticOutbound: "direct-v6-1"}},
	}

	payload, err := GenerateServerConfig(ServerInput{
		Nodes:         []InboundNode{routingTestNode("edge")},
		IPv6Outbounds: []netip.Addr{netip.MustParseAddr("2001:db8:1::10")},
		RoutingPlan:   &plan,
	})
	if err != nil {
		t.Fatalf("GenerateServerConfig() error = %v", err)
	}

	config := decodeRoutingConfig(t, payload)
	if len(config.Outbounds) != 2 {
		t.Fatalf("outbound count = %d, want direct IPv6 and direct IPv4", len(config.Outbounds))
	}
	if config.Outbounds[1]["type"] != "direct" || config.Outbounds[1]["tag"] != "direct-v4" {
		t.Fatalf("IPv4 outbound = %#v", config.Outbounds[1])
	}
	wantRules := []map[string]any{
		{
			"inbound":    []any{"in-edge-v6"},
			"ip_version": float64(6),
			"action":     "route",
			"outbound":   "direct-v6-1",
		},
		{
			"inbound":    []any{"in-edge-v6"},
			"ip_version": float64(4),
			"action":     "route",
			"outbound":   "direct-v4",
		},
	}
	if !reflect.DeepEqual(config.Route.Rules, wantRules) {
		t.Fatalf("route rules = %#v, want %#v", config.Route.Rules, wantRules)
	}
}

func TestGenerateServerConfigBuildsNonInterruptingRotationSelectors(t *testing.T) {
	plan := topology.Plan{
		Mode:     domain.RoutingModeIPv6Only,
		Topology: domain.TopologyMultiIPv6RotatingNode,
		IPv4:     topology.IPv4Policy{Action: topology.IPv4Reject},
		Nodes: []topology.NodePlan{{
			NodeID: "rotating",
			Rotation: &topology.RotationPlan{
				Candidates: []topology.OutboundCandidate{
					{Tag: "direct-v6-1", Kind: topology.CandidateDirectIPv6},
					{Tag: "direct-v6-2", Kind: topology.CandidateDirectIPv6},
				},
				StartIndex:         1,
				Interval:           time.Hour,
				NewConnectionsOnly: true,
			},
		}},
	}

	payload, err := GenerateServerConfig(ServerInput{
		Nodes: []InboundNode{routingTestNode("rotating")},
		IPv6Outbounds: []netip.Addr{
			netip.MustParseAddr("2001:db8:1::10"),
			netip.MustParseAddr("2001:db8:1::11"),
		},
		RoutingPlan: &plan,
	})
	if err != nil {
		t.Fatalf("GenerateServerConfig() error = %v", err)
	}

	config := decodeRoutingConfig(t, payload)
	selector := config.Outbounds[2]
	wantSelector := map[string]any{
		"type":                        "selector",
		"tag":                         "rotate-rotating",
		"outbounds":                   []any{"direct-v6-1", "direct-v6-2"},
		"default":                     "direct-v6-2",
		"interrupt_exist_connections": false,
	}
	if !reflect.DeepEqual(selector, wantSelector) {
		t.Fatalf("selector = %#v, want %#v", selector, wantSelector)
	}
	if config.Route.Rules[0]["outbound"] != "rotate-rotating" {
		t.Fatalf("IPv6 route = %#v", config.Route.Rules[0])
	}
	if config.Route.Rules[1]["ip_version"] != float64(4) || config.Route.Rules[1]["action"] != "reject" {
		t.Fatalf("IPv4 reject route = %#v", config.Route.Rules[1])
	}
}

func TestGenerateServerConfigRejectsUnsafeOrUnresolvableRoutingPlans(t *testing.T) {
	base := topology.Plan{
		Mode:     domain.RoutingModeClientIPv4,
		Topology: domain.TopologyMultiIPv6MultiNode,
		IPv4:     topology.IPv4Policy{Action: topology.IPv4ClientDirect},
		Nodes:    []topology.NodePlan{{NodeID: "edge", StaticOutbound: "missing-outbound"}},
	}
	input := ServerInput{
		Nodes:         []InboundNode{routingTestNode("edge")},
		IPv6Outbounds: []netip.Addr{netip.MustParseAddr("2001:db8:1::10")},
		RoutingPlan:   &base,
	}
	if _, err := GenerateServerConfig(input); err == nil || !strings.Contains(err.Error(), "missing-outbound") {
		t.Fatalf("missing outbound error = %v", err)
	}

	invalidRotation := base
	invalidRotation.Nodes = []topology.NodePlan{{
		NodeID: "edge",
		Rotation: &topology.RotationPlan{
			Candidates:         []topology.OutboundCandidate{{Tag: "direct-v6-1", Kind: topology.CandidateDirectIPv6}},
			StartIndex:         1,
			Interval:           time.Hour,
			NewConnectionsOnly: false,
		},
	}}
	input.RoutingPlan = &invalidRotation
	if _, err := GenerateServerConfig(input); err == nil {
		t.Fatal("GenerateServerConfig() accepted an interrupting or invalid rotation plan")
	}
}

type decodedRoutingConfig struct {
	Outbounds []map[string]any `json:"outbounds"`
	Route     struct {
		Rules []map[string]any `json:"rules"`
	} `json:"route"`
}

func decodeRoutingConfig(t *testing.T, payload []byte) decodedRoutingConfig {
	t.Helper()
	var config decodedRoutingConfig
	if err := json.Unmarshal(payload, &config); err != nil {
		t.Fatalf("generated JSON is invalid: %v", err)
	}
	return config
}

func routingTestNode(id string) InboundNode {
	return InboundNode{
		ID:         id,
		Protocol:   domain.ProtocolVLESS,
		Port:       24443,
		Listeners:  []netip.Addr{netip.MustParseAddr("2001:db8::10")},
		Credential: Credential{UUID: "550e8400-e29b-41d4-a716-446655440000"},
	}
}
