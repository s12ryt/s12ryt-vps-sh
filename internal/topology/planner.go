package topology

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

var outboundTagPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

type CandidateKind string

const (
	CandidateDirectIPv6  CandidateKind = "direct-ipv6"
	CandidateRemoteProxy CandidateKind = "remote-proxy"
)

type AddressFamily string

const (
	AddressFamilyIPv4 AddressFamily = "ipv4"
	AddressFamilyIPv6 AddressFamily = "ipv6"
)

type ClientAction string

const (
	ClientActionDirect ClientAction = "direct"
	ClientActionProxy  ClientAction = "proxy"
)

type IPv4Action string

const (
	IPv4ClientDirect IPv4Action = "client-direct"
	IPv4VPSFallback  IPv4Action = "vps-fallback"
	IPv4Reject       IPv4Action = "reject"
)

type OutboundCandidate struct {
	Tag  string
	Kind CandidateKind
}

type Input struct {
	Config             domain.Config
	OutboundCandidates []OutboundCandidate
	IPv4Candidates     []string
}

type ClientRule struct {
	Family AddressFamily
	Action ClientAction
}

type IPv4Policy struct {
	Action     IPv4Action
	Candidates []string
}

type RotationPlan struct {
	Candidates         []OutboundCandidate
	StartIndex         int
	Interval           time.Duration
	NewConnectionsOnly bool
}

type NodePlan struct {
	NodeID         string
	StaticOutbound string
	Rotation       *RotationPlan
}

type Plan struct {
	Mode        domain.RoutingMode
	Topology    domain.Topology
	IPv4        IPv4Policy
	ClientRules []ClientRule
	Nodes       []NodePlan
}

func BuildPlan(input Input) (Plan, error) {
	if err := input.Config.Validate(); err != nil {
		return Plan{}, fmt.Errorf("validate topology configuration: %w", err)
	}
	candidates, err := validateOutboundCandidates(input.OutboundCandidates)
	if err != nil {
		return Plan{}, err
	}
	ipv4Candidates, err := validateCandidateTags(input.IPv4Candidates, "IPv4")
	if err != nil {
		return Plan{}, err
	}

	enabledNodes := make([]domain.Node, 0, len(input.Config.Nodes))
	for _, node := range input.Config.Nodes {
		if node.Enabled {
			enabledNodes = append(enabledNodes, node)
		}
	}

	plan := Plan{
		Mode:     input.Config.Routing.Mode,
		Topology: input.Config.Routing.Topology,
	}
	if err := applyIPv4Policy(&plan, ipv4Candidates); err != nil {
		return Plan{}, err
	}
	if err := applyTopology(&plan, enabledNodes, candidates, input.Config.Routing.RotationInterval); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func validateOutboundCandidates(input []OutboundCandidate) ([]OutboundCandidate, error) {
	result := append([]OutboundCandidate(nil), input...)
	seen := make(map[string]struct{}, len(result))
	for _, candidate := range result {
		if !outboundTagPattern.MatchString(candidate.Tag) {
			return nil, fmt.Errorf("unsafe outbound candidate tag %q", candidate.Tag)
		}
		if candidate.Kind != CandidateDirectIPv6 && candidate.Kind != CandidateRemoteProxy {
			return nil, fmt.Errorf("unsupported outbound candidate kind %q", candidate.Kind)
		}
		if _, duplicate := seen[candidate.Tag]; duplicate {
			return nil, fmt.Errorf("duplicate outbound candidate %q", candidate.Tag)
		}
		seen[candidate.Tag] = struct{}{}
	}
	return result, nil
}

func validateCandidateTags(input []string, label string) ([]string, error) {
	result := append([]string(nil), input...)
	seen := make(map[string]struct{}, len(result))
	for _, candidate := range result {
		if !outboundTagPattern.MatchString(candidate) {
			return nil, fmt.Errorf("unsafe %s candidate tag %q", label, candidate)
		}
		if _, duplicate := seen[candidate]; duplicate {
			return nil, fmt.Errorf("duplicate %s candidate %q", label, candidate)
		}
		seen[candidate] = struct{}{}
	}
	return result, nil
}

func applyIPv4Policy(plan *Plan, candidates []string) error {
	switch plan.Mode {
	case domain.RoutingModeClientIPv4:
		plan.IPv4 = IPv4Policy{Action: IPv4ClientDirect}
		plan.ClientRules = []ClientRule{
			{Family: AddressFamilyIPv4, Action: ClientActionDirect},
			{Family: AddressFamilyIPv6, Action: ClientActionProxy},
		}
	case domain.RoutingModeVPSIPv4:
		if len(candidates) == 0 {
			return errors.New("VPS IPv4 mode requires at least one ordered IPv4 candidate")
		}
		plan.IPv4 = IPv4Policy{Action: IPv4VPSFallback, Candidates: append([]string(nil), candidates...)}
	case domain.RoutingModeIPv6Only:
		plan.IPv4 = IPv4Policy{Action: IPv4Reject}
	default:
		return fmt.Errorf("unsupported routing mode %q", plan.Mode)
	}
	return nil
}

func applyTopology(plan *Plan, nodes []domain.Node, candidates []OutboundCandidate, interval time.Duration) error {
	switch plan.Topology {
	case domain.TopologyMultiIPv6MultiNode:
		if len(nodes) == 0 {
			return errors.New("multi-node topology requires at least one enabled node")
		}
		if len(candidates) < len(nodes) {
			return errors.New("multi-node topology requires one independent outbound per enabled node")
		}
		for index, node := range nodes {
			plan.Nodes = append(plan.Nodes, NodePlan{NodeID: node.ID, StaticOutbound: candidates[index].Tag})
		}
	case domain.TopologySingleIPv6SingleNode:
		if len(nodes) != 1 || len(candidates) != 1 {
			return errors.New("single topology requires exactly one enabled node and one outbound")
		}
		plan.Nodes = []NodePlan{{NodeID: nodes[0].ID, StaticOutbound: candidates[0].Tag}}
	case domain.TopologyMultiIPv6RotatingNode:
		if len(nodes) != 1 {
			return errors.New("single rotating topology requires exactly one enabled node")
		}
		if len(candidates) < 2 {
			return errors.New("rotating topology requires at least two outbound candidates")
		}
		plan.Nodes = []NodePlan{rotatingNodePlan(nodes[0].ID, candidates, interval, 0)}
	case domain.TopologyMultiIPv6RotatingNodes:
		if len(nodes) < 2 {
			return errors.New("multiple rotating topology requires at least two enabled nodes")
		}
		if len(candidates) < 2 {
			return errors.New("rotating topology requires at least two outbound candidates")
		}
		for index, node := range nodes {
			plan.Nodes = append(plan.Nodes, rotatingNodePlan(node.ID, candidates, interval, index%len(candidates)))
		}
	default:
		return fmt.Errorf("unsupported topology %q", plan.Topology)
	}
	return nil
}

func rotatingNodePlan(nodeID string, candidates []OutboundCandidate, interval time.Duration, startIndex int) NodePlan {
	return NodePlan{
		NodeID: nodeID,
		Rotation: &RotationPlan{
			Candidates:         append([]OutboundCandidate(nil), candidates...),
			StartIndex:         startIndex,
			Interval:           interval,
			NewConnectionsOnly: true,
		},
	}
}
