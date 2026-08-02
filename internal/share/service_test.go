package share

import (
	"context"
	"encoding/base64"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/runtimeconfig"
	"github.com/s12ryt/s12ryt-vps-sh/internal/singbox"
)

func TestServiceBuildsProtectedDualStackShareBundle(t *testing.T) {
	config := domain.DefaultConfig()
	config.Routing.Mode = domain.RoutingModeClientIPv4
	config.Nodes = []domain.Node{
		{
			ID: "edge",
			Protocol: domain.ProtocolVLESS,
			Port: 24443,
			Enabled: true,
			Credential: domain.NodeCredential{UUID: "11111111-1111-4111-8111-111111111111"},
		},
		{
			ID: "disabled",
			Protocol: domain.ProtocolTUIC,
			Port: 24444,
			Credential: domain.NodeCredential{
				UUID: "22222222-2222-4222-8222-222222222222",
				Password: "abcdefghijklmnopqrstuvwx",
			},
		},
	}
	state := runtimeconfig.DeploymentState{
		SchemaVersion: runtimeconfig.DeploymentStateSchemaVersion,
		Nodes: []runtimeconfig.PersistedNodeDeployment{
			{
				NodeID: "edge",
				Listeners: []netip.Addr{
					netip.MustParseAddr("198.51.100.10"),
					netip.MustParseAddr("2001:db8:100::10"),
				},
				TLS: runtimeconfig.PersistedTLSConfig{
					Enabled: true,
					ServerName: "edge.example.com",
					CertificatePath: "/opt/s12ryt-ipv6/tls/server.crt",
					KeyPath: "/opt/s12ryt-ipv6/tls/server.key",
				},
				Transport: runtimeconfig.PersistedTransportConfig{Type: singbox.TransportWebSocket, Path: "/edge"},
			},
			{
				NodeID: "disabled",
				Listeners: []netip.Addr{netip.MustParseAddr("2001:db8:100::11")},
				TLS: runtimeconfig.PersistedTLSConfig{
					Enabled: true,
					ServerName: "disabled.example.com",
					CertificatePath: "/opt/s12ryt-ipv6/tls/server.crt",
					KeyPath: "/opt/s12ryt-ipv6/tls/server.key",
				},
			},
		},
		RemoteOutbounds: []runtimeconfig.PersistedRemoteOutbound{{
			Enabled: true,
			Config: []byte(`{"type":"vless","tag":"remote-secret","server":"remote.example.com","server_port":443,"uuid":"remote-secret-must-not-leak"}`),
		}},
	}
	renderer := &recordingQRRenderer{png: []byte("qr-png")}
	health := &recordingNodeHealth{healthy: map[string]bool{"edge": true}}
	service, err := NewService(ServiceOptions{
		Source: &staticShareSource{config: config, state: state},
		Health: health,
		QRRenderer: renderer,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	bundle, err := service.Bundle(context.Background())
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}
	if len(bundle.Nodes) != 3 {
		t.Fatalf("artifacts = %d, want 3", len(bundle.Nodes))
	}
	for index, expectedID := range []string{"edge-v4", "edge-v6", "disabled"} {
		if bundle.Nodes[index].NodeID != expectedID {
			t.Fatalf("artifact %d ID = %q, want %q", index, bundle.Nodes[index].NodeID, expectedID)
		}
		if string(bundle.Nodes[index].QRPNG) != "qr-png" {
			t.Fatalf("artifact %d QR PNG = %q", index, bundle.Nodes[index].QRPNG)
		}
	}
	for _, artifact := range bundle.Nodes[:2] {
		if !strings.Contains(artifact.URI, "security=tls") || !strings.Contains(artifact.URI, "type=ws") ||
			len(artifact.FullClientJSON) == 0 || artifact.FullClientBase64 == "" {
			t.Fatalf("enabled dual-stack artifact is incomplete: %#v", artifact)
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(bundle.Subscription)
	if err != nil {
		t.Fatalf("decode subscription: %v", err)
	}
	subscription := string(decoded)
	for _, artifact := range bundle.Nodes[:2] {
		if !strings.Contains(subscription, artifact.URI) {
			t.Fatalf("subscription does not contain %q", artifact.URI)
		}
	}
	for _, forbidden := range []string{"disabled", "remote-secret", "remote-secret-must-not-leak"} {
		if strings.Contains(subscription, forbidden) {
			t.Fatalf("subscription leaked %q: %s", forbidden, subscription)
		}
	}
	if len(health.calls) != 1 || health.calls[0] != "edge" {
		t.Fatalf("health calls = %#v, want enabled node only", health.calls)
	}
}

func TestServiceRejectsInconsistentSnapshotsAndHealthFailures(t *testing.T) {
	config := domain.DefaultConfig()
	config.Nodes = []domain.Node{{
		ID: "edge",
		Protocol: domain.ProtocolVLESS,
		Port: 24443,
		Enabled: true,
		Credential: domain.NodeCredential{UUID: "11111111-1111-4111-8111-111111111111"},
	}}
	baseState := runtimeconfig.DeploymentState{
		SchemaVersion: runtimeconfig.DeploymentStateSchemaVersion,
		Nodes: []runtimeconfig.PersistedNodeDeployment{{
			NodeID: "edge",
			Listeners: []netip.Addr{netip.MustParseAddr("2001:db8:100::10")},
		}},
	}
	sentinel := errors.New("health failed")
	tests := map[string]struct {
		state  runtimeconfig.DeploymentState
		health NodeHealth
	}{
		"missing deployment": {state: runtimeconfig.DeploymentState{SchemaVersion: runtimeconfig.DeploymentStateSchemaVersion}, health: &recordingNodeHealth{}},
		"unknown deployment": {
			state: runtimeconfig.DeploymentState{
				SchemaVersion: runtimeconfig.DeploymentStateSchemaVersion,
				Nodes: []runtimeconfig.PersistedNodeDeployment{{
					NodeID: "unknown",
					Listeners: []netip.Addr{netip.MustParseAddr("2001:db8:100::12")},
				}},
			},
			health: &recordingNodeHealth{},
		},
		"health error": {state: baseState, health: &recordingNodeHealth{err: sentinel}},
	}
	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			service, err := NewService(ServiceOptions{Source: &staticShareSource{config: config, state: testCase.state}, Health: testCase.health})
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			_, err = service.Bundle(context.Background())
			if err == nil {
				t.Fatal("Bundle() accepted inconsistent protected state")
			}
			if name == "health error" && !errors.Is(err, sentinel) {
				t.Fatalf("Bundle() error = %v, want sentinel", err)
			}
		})
	}
}

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	health := &recordingNodeHealth{}
	source := &staticShareSource{}
	for name, options := range map[string]ServiceOptions{
		"source": {Health: health},
		"health": {Source: source},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewService(options); err == nil {
				t.Fatal("NewService() accepted a missing dependency")
			}
		})
	}
}

type staticShareSource struct {
	config domain.Config
	state  runtimeconfig.DeploymentState
}

func (source *staticShareSource) Snapshot() domain.Config {
	return source.config
}

func (source *staticShareSource) RuntimeSnapshot() runtimeconfig.DeploymentState {
	return source.state
}

type recordingNodeHealth struct {
	calls   []string
	healthy map[string]bool
	err     error
}

func (health *recordingNodeHealth) Healthy(_ context.Context, nodeID string) (bool, error) {
	health.calls = append(health.calls, nodeID)
	if health.err != nil {
		return false, health.err
	}
	return health.healthy[nodeID], nil
}
