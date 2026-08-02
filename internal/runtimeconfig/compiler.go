package runtimeconfig

import (
	"fmt"
	"net/netip"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/singbox"
	"github.com/s12ryt/s12ryt-vps-sh/internal/topology"
)

type Input struct {
	Config          domain.Config
	Deployments     []NodeDeployment
	IPv6Outbounds   []netip.Addr
	RemoteOutbounds []RemoteOutbound
	IPv4Fallback    []string
}

type RemoteOutbound struct {
	Tag     string
	Type    string
	Enabled bool
	Config  map[string]any
}

type NodeDeployment struct {
	NodeID    string
	Listeners []netip.Addr
	TLS       singbox.TLSConfig
	Transport singbox.TransportConfig
}

func CompileServerConfig(input Input) ([]byte, error) {
	if err := input.Config.Validate(); err != nil {
		return nil, fmt.Errorf("validate runtime configuration: %w", err)
	}

	nodesByID := make(map[string]domain.Node, len(input.Config.Nodes))
	for _, node := range input.Config.Nodes {
		nodesByID[node.ID] = node
	}
	deployments := make(map[string]NodeDeployment, len(input.Deployments))
	for _, deployment := range input.Deployments {
		if _, exists := nodesByID[deployment.NodeID]; !exists {
			return nil, fmt.Errorf("deployment references unknown node %q", deployment.NodeID)
		}
		if _, duplicate := deployments[deployment.NodeID]; duplicate {
			return nil, fmt.Errorf("duplicate deployment for node %q", deployment.NodeID)
		}
		deployments[deployment.NodeID] = deployment
	}

	enabled := make([]domain.Node, 0, len(input.Config.Nodes))
	for _, node := range input.Config.Nodes {
		if node.Enabled {
			enabled = append(enabled, node)
		}
	}
	serverNodes := make([]singbox.InboundNode, 0, len(enabled))
	for _, node := range enabled {
		deployment, exists := deployments[node.ID]
		if !exists {
			return nil, fmt.Errorf("enabled node %q is missing deployment data", node.ID)
		}
		serverNodes = append(serverNodes, singbox.InboundNode{
			ID:         node.ID,
			Protocol:   node.Protocol,
			Port:       node.Port,
			Listeners:  append([]netip.Addr(nil), deployment.Listeners...),
			Credential: mapCredential(node.Credential),
			TLS:        deployment.TLS,
			Transport:  deployment.Transport,
		})
	}

	resolvedOutbounds := resolveIPv6Outbounds(input.IPv6Outbounds, enabled, deployments)
	var routingPlan *topology.Plan
	if len(enabled) > 0 {
		candidates := make([]topology.OutboundCandidate, 0, len(resolvedOutbounds)+len(input.RemoteOutbounds))
		for index := range resolvedOutbounds {
			candidates = append(candidates, topology.OutboundCandidate{
				Tag:  fmt.Sprintf("direct-v6-%d", index+1),
				Kind: topology.CandidateDirectIPv6,
			})
		}
		for _, outbound := range input.RemoteOutbounds {
			if outbound.Enabled {
				candidates = append(candidates, topology.OutboundCandidate{
					Tag:  outbound.Tag,
					Kind: topology.CandidateRemoteProxy,
				})
			}
		}
		var ipv4Candidates []string
		if input.Config.Routing.Mode == domain.RoutingModeVPSIPv4 {
			ipv4Candidates = append([]string(nil), input.IPv4Fallback...)
			if len(ipv4Candidates) == 0 {
				ipv4Candidates = []string{"direct-v4"}
			}
		}
		plan, err := topology.BuildPlan(topology.Input{
			Config:             input.Config,
			OutboundCandidates: candidates,
			IPv4Candidates:     ipv4Candidates,
		})
		if err != nil {
			return nil, fmt.Errorf("build runtime routing plan: %w", err)
		}
		routingPlan = &plan
	}

	return singbox.GenerateServerConfig(singbox.ServerInput{
		Nodes:           serverNodes,
		IPv6Outbounds:   resolvedOutbounds,
		RemoteOutbounds: remoteOutboundMaps(input.RemoteOutbounds),
		RoutingPlan:     routingPlan,
	})
}

func remoteOutboundMaps(input []RemoteOutbound) []map[string]any {
	result := make([]map[string]any, 0, len(input))
	for _, outbound := range input {
		result = append(result, cloneOutboundMap(outbound.Config))
	}
	return result
}

func resolveIPv6Outbounds(
	explicit []netip.Addr,
	enabled []domain.Node,
	deployments map[string]NodeDeployment,
) []netip.Addr {
	if len(explicit) > 0 {
		return append([]netip.Addr(nil), explicit...)
	}
	result := make([]netip.Addr, 0, len(enabled))
	seen := make(map[netip.Addr]struct{}, len(enabled))
	for _, node := range enabled {
		deployment, exists := deployments[node.ID]
		if !exists {
			continue
		}
		for _, listener := range deployment.Listeners {
			if !listener.Is6() || !listener.IsGlobalUnicast() {
				continue
			}
			if _, duplicate := seen[listener]; duplicate {
				continue
			}
			seen[listener] = struct{}{}
			result = append(result, listener)
		}
	}
	return result
}

func mapCredential(credential domain.NodeCredential) singbox.Credential {
	return singbox.Credential{
		Username: credential.Username,
		UUID:     credential.UUID,
		Password: credential.Password,
		Method:   credential.Method,
	}
}
