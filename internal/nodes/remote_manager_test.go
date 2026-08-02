package nodes

import (
	"bytes"
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/runtimeconfig"
)

func TestImportRemoteOutboundsPersistsSecretsButReturnsMaskedSummaries(t *testing.T) {
	applier := &recordingDeploymentApplier{}
	manager := newEmptyRuntimeManagedTestManager(t, applier)
	payload := strings.Join([]string{
		"vless://550e8400-e29b-41d4-a716-446655440000@remote.example.com:443?security=tls&sni=remote.example.com",
		"socks5://proxy-user:proxy-secret@192.0.2.20:1080",
	}, "\n")

	summaries, err := manager.ImportRemoteOutbounds(ImportRemoteInput{
		Payload:        []byte(payload),
		AllowIPv4Proxy: true,
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("ImportRemoteOutbounds() error = %v", err)
	}
	if len(summaries) != 2 || summaries[0].Tag != "remote-1" || summaries[1].Type != "socks" {
		t.Fatalf("summaries = %#v", summaries)
	}
	masked := strings.Join([]string{summaries[0].Tag, summaries[0].Type, summaries[0].Server, summaries[1].Tag, summaries[1].Type, summaries[1].Server}, " ")
	if strings.Contains(masked, "proxy-secret") || strings.Contains(masked, "550e8400") {
		t.Fatalf("summaries leaked credentials: %#v", summaries)
	}
	if len(applier.calls) != 1 || len(applier.calls[0].candidateState.RemoteOutbounds) != 2 {
		t.Fatalf("deployment calls = %#v", applier.calls)
	}
	persisted := string(applier.calls[0].candidateState.RemoteOutbounds[1].Config)
	if !strings.Contains(persisted, "proxy-secret") {
		t.Fatalf("protected runtime state omitted credential: %s", persisted)
	}
}

func TestRemoteOutboundUpdatesAndOrderedIPv4FallbackUseOneTransaction(t *testing.T) {
	applier := &recordingDeploymentApplier{}
	manager := newEmptyRuntimeManagedTestManager(t, applier)
	_, err := manager.ImportRemoteOutbounds(ImportRemoteInput{
		Payload:        []byte("socks5://user:secret@192.0.2.30:1080\nhttps://user:secret@proxy.example.com:8443"),
		AllowIPv4Proxy: true,
		Enabled:        false,
	})
	if err != nil {
		t.Fatalf("ImportRemoteOutbounds() error = %v", err)
	}
	if _, err := manager.UpdateRemoteOutbound("remote-1", true); err != nil {
		t.Fatalf("UpdateRemoteOutbound() error = %v", err)
	}
	wantFallback := []string{"remote-2", "remote-1", "direct-v4"}
	if err := manager.SetIPv4Fallback(wantFallback); err != nil {
		t.Fatalf("SetIPv4Fallback() error = %v", err)
	}
	state := manager.RuntimeSnapshot()
	if !state.RemoteOutbounds[0].Enabled || !reflect.DeepEqual(state.IPv4Fallback, wantFallback) {
		t.Fatalf("runtime state = %#v", state)
	}
	if len(applier.calls) != 3 {
		t.Fatalf("deployment call count = %d, want 3", len(applier.calls))
	}
}

func TestDeleteRemoteOutboundRejectsActiveFallbackThenRemovesExactEntry(t *testing.T) {
	manager := newEmptyRuntimeManagedTestManager(t, &recordingDeploymentApplier{})
	_, err := manager.ImportRemoteOutbounds(ImportRemoteInput{
		Payload:        []byte("socks5://user:secret@192.0.2.40:1080"),
		AllowIPv4Proxy: true,
		Enabled:        true,
	})
	if err != nil {
		t.Fatalf("ImportRemoteOutbounds() error = %v", err)
	}
	if err := manager.SetIPv4Fallback([]string{"remote-1", "direct-v4"}); err != nil {
		t.Fatalf("SetIPv4Fallback() error = %v", err)
	}
	if err := manager.DeleteRemoteOutbound("remote-1"); err == nil {
		t.Fatal("DeleteRemoteOutbound() removed an active fallback")
	}
	if err := manager.SetIPv4Fallback([]string{"direct-v4"}); err != nil {
		t.Fatalf("SetIPv4Fallback(direct) error = %v", err)
	}
	if err := manager.DeleteRemoteOutbound("remote-1"); err != nil {
		t.Fatalf("DeleteRemoteOutbound() error = %v", err)
	}
	if len(manager.RuntimeSnapshot().RemoteOutbounds) != 0 {
		t.Fatal("remote outbound remains after deletion")
	}
}

func TestRemoteMutationFailurePreservesProtectedRuntimeState(t *testing.T) {
	applyErr := errors.New("runtime reload failed")
	applier := &recordingDeploymentApplier{err: applyErr}
	manager := newEmptyRuntimeManagedTestManager(t, applier)
	before := manager.RuntimeSnapshot()

	_, err := manager.ImportRemoteOutbounds(ImportRemoteInput{
		Payload: []byte("vless://550e8400-e29b-41d4-a716-446655440000@remote.example.com:443"),
		Enabled: true,
	})
	if !errors.Is(err, applyErr) {
		t.Fatalf("ImportRemoteOutbounds() error = %v, want %v", err, applyErr)
	}
	if !reflect.DeepEqual(manager.RuntimeSnapshot(), before) {
		t.Fatal("failed remote mutation changed runtime state")
	}
}

func TestRemoteManagementRejectsLegacyOrUnsafeOperations(t *testing.T) {
	legacy, err := NewManager(ManagerOptions{
		Config:       domain.DefaultConfig(),
		Store:        &recordingStore{},
		Entropy:      bytes.NewReader(bytes.Repeat([]byte{0x74}, 128)),
		AllocatePort: func() (int, error) { return 24443, nil },
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if _, err := legacy.ImportRemoteOutbounds(ImportRemoteInput{Payload: []byte("vless://bad")}); err == nil {
		t.Fatal("legacy manager accepted remote runtime mutation")
	}

	managed := newEmptyRuntimeManagedTestManager(t, &recordingDeploymentApplier{})
	if _, err := managed.ImportRemoteOutbounds(ImportRemoteInput{Payload: []byte("https://subscriptions.example/list")}); err == nil {
		t.Fatal("manager accepted a subscription URL")
	}
	if _, err := managed.UpdateRemoteOutbound("missing", true); err == nil {
		t.Fatal("manager updated an unknown remote outbound")
	}
	if err := managed.SetIPv4Fallback([]string{"missing"}); err == nil {
		t.Fatal("manager accepted an unknown IPv4 fallback")
	}
}

func newEmptyRuntimeManagedTestManager(t *testing.T, applier DeploymentApplier) *Manager {
	t.Helper()
	state := runtimeconfig.DeploymentState{
		SchemaVersion: runtimeconfig.DeploymentStateSchemaVersion,
		Nodes:         []runtimeconfig.PersistedNodeDeployment{},
		IPv6Outbounds: []netip.Addr{},
	}
	manager, err := NewManager(ManagerOptions{
		Config:            domain.DefaultConfig(),
		Store:             &recordingStore{},
		Entropy:           bytes.NewReader(bytes.Repeat([]byte{0x73}, 4096)),
		AllocatePort:      func() (int, error) { return 24443, nil },
		RuntimeState:      &state,
		DeploymentApplier: applier,
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager
}
