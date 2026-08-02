package networksetup

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/manifest"
	projectnetwork "github.com/s12ryt/s12ryt-vps-sh/internal/network"
)

func TestCoordinatorDiscoversGeneratesAndAppliesProtectedIntegrationManifest(t *testing.T) {
	events := make([]string, 0)
	discovery := &recordingDiscovery{
		events: &events,
		addresses: []projectnetwork.InterfaceAddress{
			{Interface: "eth0", Prefix: netip.MustParsePrefix("2001:db8:100::1/64")},
			{Interface: "eth1", Prefix: netip.MustParsePrefix("2001:db8:200::1/64")},
		},
		gateway: netip.MustParseAddr("fe80::1"),
	}
	applier := &recordingApplier{events: &events}
	coordinator, err := NewCoordinator(CoordinatorOptions{
		Discovery: discovery,
		Applier:   applier,
		Entropy: bytes.NewReader(append(
			entropyAddress(1),
			append(entropyAddress(2), entropyAddress(3)...)...,
		)),
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	result, err := coordinator.Apply(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !reflect.DeepEqual(events, []string{"addresses", "gateway:eth0", "apply"}) {
		t.Fatalf("events = %#v", events)
	}
	want := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Interface:     "eth0",
		Prefix:        "2001:db8:100::/64",
		Gateway:       "fe80::1",
		Addresses:     []string{"2001:db8:100::2", "2001:db8:100::3"},
		Firewall: manifest.FirewallManifest{
			Backend:      projectnetwork.FirewallNFTables,
			PanelPort:    34456,
			AllowedCIDRs: []string{"0.0.0.0/0", "::/0"},
			NodePorts: []manifest.PortManifest{
				{Port: 24001, Protocol: "tcp"},
				{Port: 24002, Protocol: "udp"},
			},
		},
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("Apply() manifest = %#v, want %#v", result, want)
	}
	if !reflect.DeepEqual(applier.applied, want) {
		t.Fatalf("applied manifest = %#v, want %#v", applier.applied, want)
	}
}

func TestCoordinatorRejectsUnsafeRequestBeforeDiscoveryOrApply(t *testing.T) {
	for name, mutate := range map[string]func(*Request){
		"interface":        func(value *Request) { value.Interface = "eth0;reboot" },
		"IPv4 prefix":      func(value *Request) { value.Prefix = "198.51.100.0/24" },
		"no host space":    func(value *Request) { value.Prefix = "2001:db8:100::1/128" },
		"zero count":       func(value *Request) { value.Count = 0 },
		"excessive count":  func(value *Request) { value.Count = 257 },
		"firewall backend": func(value *Request) { value.FirewallBackend = "iptables" },
		"panel port":       func(value *Request) { value.PanelPort = 0 },
		"allowed CIDR":     func(value *Request) { value.AllowedCIDRs = []string{"not-a-cidr"} },
		"node port":        func(value *Request) { value.NodePorts[0].Port = 443 },
	} {
		t.Run(name, func(t *testing.T) {
			events := make([]string, 0)
			coordinator, err := NewCoordinator(CoordinatorOptions{
				Discovery: &recordingDiscovery{events: &events},
				Applier:   &recordingApplier{events: &events},
				Entropy:   bytes.NewReader(entropyAddress(1)),
			})
			if err != nil {
				t.Fatalf("NewCoordinator() error = %v", err)
			}
			request := validRequest()
			mutate(&request)
			if _, err := coordinator.Apply(context.Background(), request); err == nil {
				t.Fatal("Apply() error = nil, want rejection")
			}
			if len(events) != 0 {
				t.Fatalf("unsafe request caused side effects: %#v", events)
			}
		})
	}
}

func TestCoordinatorPreservesDiscoveryEntropyAndApplyErrors(t *testing.T) {
	discoveryErr := errors.New("discovery failed")
	entropyErr := errors.New("entropy failed")
	applyErr := errors.New("apply failed")

	for name, options, want := range map[string]struct {
		options CoordinatorOptions
		want    error
	}{
		"addresses": {
			options: CoordinatorOptions{
				Discovery: &recordingDiscovery{addressesErr: discoveryErr},
				Applier:   &recordingApplier{},
				Entropy:   bytes.NewReader(entropyAddress(1)),
			},
			want: discoveryErr,
		},
		"gateway": {
			options: CoordinatorOptions{
				Discovery: &recordingDiscovery{gatewayErr: discoveryErr},
				Applier:   &recordingApplier{},
				Entropy:   bytes.NewReader(entropyAddress(1)),
			},
			want: discoveryErr,
		},
		"entropy": {
			options: CoordinatorOptions{
				Discovery: &recordingDiscovery{gateway: netip.MustParseAddr("fe80::1")},
				Applier:   &recordingApplier{},
				Entropy:   errorReader{err: entropyErr},
			},
			want: entropyErr,
		},
		"apply": {
			options: CoordinatorOptions{
				Discovery: &recordingDiscovery{gateway: netip.MustParseAddr("fe80::1")},
				Applier:   &recordingApplier{err: applyErr},
				Entropy:   bytes.NewReader(entropyAddress(1)),
			},
			want: applyErr,
		},
	} {
		t.Run(name, func(t *testing.T) {
			coordinator, err := NewCoordinator(options)
			if err != nil {
				t.Fatalf("NewCoordinator() error = %v", err)
			}
			if _, err := coordinator.Apply(context.Background(), validRequest()); !errors.Is(err, want) {
				t.Fatalf("Apply() error = %v, want errors.Is(%v)", err, want)
			}
		})
	}
}

func TestNewCoordinatorRejectsMissingDependencies(t *testing.T) {
	valid := CoordinatorOptions{
		Discovery: &recordingDiscovery{},
		Applier:   &recordingApplier{},
		Entropy:   bytes.NewReader(entropyAddress(1)),
	}
	for name, mutate := range map[string]func(*CoordinatorOptions){
		"discovery": func(value *CoordinatorOptions) { value.Discovery = nil },
		"applier":   func(value *CoordinatorOptions) { value.Applier = nil },
		"entropy":   func(value *CoordinatorOptions) { value.Entropy = nil },
	} {
		t.Run(name, func(t *testing.T) {
			options := valid
			mutate(&options)
			if _, err := NewCoordinator(options); err == nil {
				t.Fatal("NewCoordinator() error = nil, want rejection")
			}
		})
	}
}

type recordingDiscovery struct {
	events       *[]string
	addresses    []projectnetwork.InterfaceAddress
	addressesErr error
	gateway      netip.Addr
	gatewayErr   error
}

func (discovery *recordingDiscovery) GlobalIPv6Addresses(context.Context) ([]projectnetwork.InterfaceAddress, error) {
	discovery.record("addresses")
	return append([]projectnetwork.InterfaceAddress(nil), discovery.addresses...), discovery.addressesErr
}

func (discovery *recordingDiscovery) UniqueIPv6Gateway(_ context.Context, interfaceName string) (netip.Addr, error) {
	discovery.record("gateway:" + interfaceName)
	return discovery.gateway, discovery.gatewayErr
}

func (discovery *recordingDiscovery) record(event string) {
	if discovery.events != nil {
		*discovery.events = append(*discovery.events, event)
	}
}

type recordingApplier struct {
	events  *[]string
	applied manifest.Manifest
	err     error
}

func (applier *recordingApplier) Apply(_ context.Context, value manifest.Manifest) error {
	if applier.events != nil {
		*applier.events = append(*applier.events, "apply")
	}
	applier.applied = value
	return applier.err
}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

func validRequest() Request {
	return Request{
		Interface:       "eth0",
		Prefix:          "2001:db8:100::/64",
		Count:           2,
		FirewallBackend: projectnetwork.FirewallNFTables,
		PanelPort:       34456,
		AllowedCIDRs:    []string{"0.0.0.0/0", "::/0"},
		NodePorts: []manifest.PortManifest{
			{Port: 24001, Protocol: "tcp"},
			{Port: 24002, Protocol: "udp"},
		},
	}
}

func entropyAddress(last byte) []byte {
	value := make([]byte, 16)
	value[len(value)-1] = last
	return value
}
