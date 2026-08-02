package resourceprofile

import (
	"errors"
	"fmt"
	"io"
	"net/netip"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/runtimeconfig"
)

const ResourceIPv6Count = 64
const ResourceNodeCount = 28
const ResourceBasePort = 23000

var resourcePrefix = netip.MustParsePrefix("2001:db8:ffff::/64")

type Profile struct {
	Config    domain.Config
	State     runtimeconfig.DeploymentState
	Addresses []netip.Addr
}

func Build(entropy io.Reader) (Profile, error) {
	if entropy == nil {
		return Profile{}, errors.New("resource profile entropy is required")
	}

	addresses, err := resourceAddresses()
	if err != nil {
		return Profile{}, err
	}
	config := domain.DefaultConfig()
	config.IPv6.GeneratedCount = ResourceIPv6Count
	config.Nodes = make([]domain.Node, 0, ResourceNodeCount)
	state := runtimeconfig.DeploymentState{
		SchemaVersion: runtimeconfig.DeploymentStateSchemaVersion,
		Nodes:         make([]runtimeconfig.PersistedNodeDeployment, 0, ResourceNodeCount),
		IPv6Outbounds: append([]netip.Addr(nil), addresses...),
	}

	for index := 0; index < ResourceNodeCount; index++ {
		credential, credentialErr := domain.GenerateNodeCredential(domain.ProtocolVLESS, entropy)
		if credentialErr != nil {
			return Profile{}, fmt.Errorf("generate resource node %d credential: %w", index+1, credentialErr)
		}
		nodeID := fmt.Sprintf("resource-%02d", index+1)
		config.Nodes = append(config.Nodes, domain.Node{
			ID:         nodeID,
			Protocol:   domain.ProtocolVLESS,
			Port:       ResourceBasePort + index,
			Enabled:    true,
			Credential: credential,
		})
		state.Nodes = append(state.Nodes, runtimeconfig.PersistedNodeDeployment{
			NodeID:    nodeID,
			Listeners: []netip.Addr{addresses[index]},
		})
	}

	if err := config.Validate(); err != nil {
		return Profile{}, fmt.Errorf("validate resource config: %w", err)
	}
	if _, err := state.Resolve(config); err != nil {
		return Profile{}, fmt.Errorf("validate resource deployment state: %w", err)
	}
	return Profile{
		Config:    config,
		State:     state,
		Addresses: append([]netip.Addr(nil), addresses...),
	}, nil
}

func resourceAddresses() ([]netip.Addr, error) {
	addresses := make([]netip.Addr, 0, ResourceIPv6Count)
	address := resourcePrefix.Addr()
	for index := 0; index < ResourceIPv6Count; index++ {
		address = address.Next()
		if !address.IsValid() || !resourcePrefix.Contains(address) {
			return nil, errors.New("resource IPv6 prefix does not contain enough addresses")
		}
		addresses = append(addresses, address)
	}
	return addresses, nil
}
