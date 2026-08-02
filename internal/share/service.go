package share

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/runtimeconfig"
	"github.com/s12ryt/s12ryt-vps-sh/internal/singbox"
)

type SnapshotSource interface {
	Snapshot() domain.Config
	RuntimeSnapshot() runtimeconfig.DeploymentState
}

type NodeHealth interface {
	Healthy(context.Context, string) (bool, error)
}

type ServiceOptions struct {
	Source     SnapshotSource
	Health     NodeHealth
	QRRenderer QRRenderer
}

type Service struct {
	source     SnapshotSource
	health     NodeHealth
	qrRenderer QRRenderer
}

func NewService(options ServiceOptions) (*Service, error) {
	if options.Source == nil {
		return nil, errors.New("share snapshot source is required")
	}
	if options.Health == nil {
		return nil, errors.New("node health provider is required")
	}
	return &Service{
		source:     options.Source,
		health:     options.Health,
		qrRenderer: options.QRRenderer,
	}, nil
}

func (service *Service) Bundle(ctx context.Context) (Bundle, error) {
	if ctx == nil {
		return Bundle{}, errors.New("share context is required")
	}
	config := service.source.Snapshot()
	if err := config.Validate(); err != nil {
		return Bundle{}, fmt.Errorf("validate share configuration: %w", err)
	}
	state := service.source.RuntimeSnapshot()
	if err := state.Validate(); err != nil {
		return Bundle{}, fmt.Errorf("validate share deployment state: %w", err)
	}

	nodes := make(map[string]domain.Node, len(config.Nodes))
	for _, node := range config.Nodes {
		nodes[node.ID] = node
	}
	deployments := make(map[string]runtimeconfig.PersistedNodeDeployment, len(state.Nodes))
	for _, deployment := range state.Nodes {
		if _, exists := nodes[deployment.NodeID]; !exists {
			return Bundle{}, fmt.Errorf("deployment references unknown node %q", deployment.NodeID)
		}
		deployments[deployment.NodeID] = deployment
	}

	localNodes := make([]LocalNode, 0, len(state.Nodes)*2)
	for _, node := range config.Nodes {
		deployment, exists := deployments[node.ID]
		if !exists {
			return Bundle{}, fmt.Errorf("node %q is missing deployment state", node.ID)
		}
		healthy := false
		if node.Enabled {
			var err error
			healthy, err = service.health.Healthy(ctx, node.ID)
			if err != nil {
				return Bundle{}, fmt.Errorf("check node %q health: %w", node.ID, err)
			}
		}
		for _, listener := range deployment.Listeners {
			localNodes = append(localNodes, localShareNode(node, deployment, listener, healthy))
		}
	}

	bundle, err := GenerateBundle(Input{
		LocalNodes:  localNodes,
		RoutingMode: config.Routing.Mode,
		QRRenderer:  service.qrRenderer,
	})
	if err != nil {
		return Bundle{}, fmt.Errorf("generate protected share bundle: %w", err)
	}
	return bundle, nil
}

func localShareNode(node domain.Node, deployment runtimeconfig.PersistedNodeDeployment, address netip.Addr, healthy bool) LocalNode {
	id := node.ID
	name := node.ID
	if len(deployment.Listeners) > 1 {
		family := "v6"
		label := "IPv6"
		if address.Is4() {
			family = "v4"
			label = "IPv4"
		}
		id += "-" + family
		name += " " + label
	}
	transportType := deployment.Transport.Type
	if transportType == singbox.TransportWebSocket {
		transportType = "ws"
	}
	return LocalNode{
		ID:       id,
		Name:     name,
		Protocol: node.Protocol,
		Server:   address,
		Port:     node.Port,
		Username: node.Credential.Username,
		UUID:     node.Credential.UUID,
		Password: node.Credential.Password,
		Method:   node.Credential.Method,
		Enabled:  node.Enabled,
		Healthy:  healthy,
		TLS:      TLSOptions{ServerName: deployment.TLS.ServerName},
		Transport: TransportOptions{
			Type:        transportType,
			Path:        deployment.Transport.Path,
			ServiceName: deployment.Transport.ServiceName,
		},
	}
}
