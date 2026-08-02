package runtimeconfig

import (
	"fmt"
	"net/netip"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/singbox"
)

type Input struct {
	Config        domain.Config
	Deployments   []NodeDeployment
	IPv6Outbounds []netip.Addr
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
	if input.Config.Routing.Topology == domain.TopologySingleIPv6SingleNode {
		if len(enabled) != 1 || len(input.IPv6Outbounds) != 1 {
			return nil, fmt.Errorf("single IPv6 single node topology requires exactly one enabled node and one IPv6 outbound")
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

	return singbox.GenerateServerConfig(singbox.ServerInput{
		Nodes:         serverNodes,
		IPv6Outbounds: append([]netip.Addr(nil), input.IPv6Outbounds...),
	})
}

func mapCredential(credential domain.NodeCredential) singbox.Credential {
	return singbox.Credential{
		Username: credential.Username,
		UUID:     credential.UUID,
		Password: credential.Password,
		Method:   credential.Method,
	}
}
