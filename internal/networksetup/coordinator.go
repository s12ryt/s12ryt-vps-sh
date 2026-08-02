package networksetup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"regexp"

	"github.com/s12ryt/s12ryt-vps-sh/internal/manifest"
	projectnetwork "github.com/s12ryt/s12ryt-vps-sh/internal/network"
)

var interfaceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

type Discovery interface {
	GlobalIPv6Addresses(context.Context) ([]projectnetwork.InterfaceAddress, error)
	UniqueIPv6Gateway(context.Context, string) (netip.Addr, error)
}

type Applier interface {
	Apply(context.Context, manifest.Manifest) error
}

type Request struct {
	Interface       string
	Prefix          string
	Count           int
	FirewallBackend string
	PanelPort       int
	AllowedCIDRs    []string
	NodePorts       []manifest.PortManifest
}

type CoordinatorOptions struct {
	Discovery Discovery
	Applier   Applier
	Entropy   io.Reader
}

type Coordinator struct {
	discovery Discovery
	applier   Applier
	entropy   io.Reader
}

func NewCoordinator(options CoordinatorOptions) (*Coordinator, error) {
	if options.Discovery == nil {
		return nil, errors.New("network discovery is required")
	}
	if options.Applier == nil {
		return nil, errors.New("network integration applier is required")
	}
	if options.Entropy == nil {
		return nil, errors.New("network setup entropy is required")
	}
	return &Coordinator{
		discovery: options.Discovery,
		applier:   options.Applier,
		entropy:   options.Entropy,
	}, nil
}

func (coordinator *Coordinator) Apply(ctx context.Context, request Request) (manifest.Manifest, error) {
	if ctx == nil {
		return manifest.Manifest{}, errors.New("network setup context is required")
	}
	prefix, err := validateRequest(request)
	if err != nil {
		return manifest.Manifest{}, err
	}

	existing, err := coordinator.discovery.GlobalIPv6Addresses(ctx)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("discover global IPv6 addresses: %w", err)
	}
	gateway, err := coordinator.discovery.UniqueIPv6Gateway(ctx, request.Interface)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("discover IPv6 gateway: %w", err)
	}
	pool, err := projectnetwork.GenerateIPv6Pool(prefix, request.Count, discoveredAddresses(existing), coordinator.entropy)
	if err != nil {
		return manifest.Manifest{}, fmt.Errorf("generate project IPv6 pool: %w", err)
	}

	candidate := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Interface:     request.Interface,
		Prefix:        prefix.String(),
		Gateway:       gateway.String(),
		Addresses:     addressStrings(pool),
		Firewall: manifest.FirewallManifest{
			Backend:      request.FirewallBackend,
			PanelPort:    request.PanelPort,
			AllowedCIDRs: append([]string(nil), request.AllowedCIDRs...),
			NodePorts:    append([]manifest.PortManifest(nil), request.NodePorts...),
		},
	}
	if err := candidate.Validate(); err != nil {
		return manifest.Manifest{}, fmt.Errorf("validate generated network integration: %w", err)
	}
	if err := coordinator.applier.Apply(ctx, candidate); err != nil {
		return manifest.Manifest{}, fmt.Errorf("apply network integration: %w", err)
	}
	return candidate, nil
}

func validateRequest(request Request) (netip.Prefix, error) {
	if request.Interface == "" || !interfaceNamePattern.MatchString(request.Interface) {
		return netip.Prefix{}, errors.New("network interface name is invalid")
	}
	prefix, err := netip.ParsePrefix(request.Prefix)
	if err != nil || !prefix.Addr().Is6() || !prefix.Addr().IsGlobalUnicast() || prefix.Bits() >= 128 {
		return netip.Prefix{}, errors.New("network prefix must be global IPv6 with host space")
	}
	if request.Count < 1 || request.Count > 256 {
		return netip.Prefix{}, errors.New("IPv6 address count must be between 1 and 256")
	}
	ports := make([]projectnetwork.PortRule, 0, len(request.NodePorts))
	for _, port := range request.NodePorts {
		ports = append(ports, projectnetwork.PortRule{Port: port.Port, Protocol: port.Protocol})
	}
	if _, err := projectnetwork.BuildFirewallPlan(projectnetwork.FirewallInput{
		Backend:      request.FirewallBackend,
		PanelPort:    request.PanelPort,
		AllowedCIDRs: append([]string(nil), request.AllowedCIDRs...),
		NodePorts:    ports,
	}); err != nil {
		return netip.Prefix{}, fmt.Errorf("validate network firewall request: %w", err)
	}
	return prefix.Masked(), nil
}

func discoveredAddresses(addresses []projectnetwork.InterfaceAddress) []netip.Addr {
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		if address.Prefix.IsValid() {
			result = append(result, address.Prefix.Addr())
		}
	}
	return result
}

func addressStrings(addresses []netip.Addr) []string {
	result := make([]string, len(addresses))
	for index, address := range addresses {
		result[index] = address.String()
	}
	return result
}
