package nodes

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/runtimeconfig"
)

func TestRuntimeManagedCreateCommitsConfigAndDeploymentTogether(t *testing.T) {
	applier := &recordingDeploymentApplier{}
	manager := newRuntimeManagedTestManager(t, applier)
	deployment := runtimeconfig.PersistedNodeDeployment{
		NodeID:    "edge-vless",
		Listeners: []netip.Addr{netip.MustParseAddr("198.51.100.10")},
	}

	node, err := manager.Create(CreateInput{
		ID: "edge-vless", Protocol: domain.ProtocolVLESS, Port: 24443, Enabled: true,
		Deployment: deployment,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(applier.calls) != 1 {
		t.Fatalf("deployment calls = %d, want 1", len(applier.calls))
	}
	call := applier.calls[0]
	if len(call.currentConfig.Nodes) != 0 || len(call.currentState.Nodes) != 0 {
		t.Fatalf("current deployment snapshot = %#v / %#v", call.currentConfig.Nodes, call.currentState.Nodes)
	}
	if !reflect.DeepEqual(call.candidateConfig.Nodes, []domain.Node{node}) {
		t.Fatalf("candidate config nodes = %#v", call.candidateConfig.Nodes)
	}
	if !reflect.DeepEqual(call.candidateState.Nodes, []runtimeconfig.PersistedNodeDeployment{deployment}) {
		t.Fatalf("candidate deployment nodes = %#v", call.candidateState.Nodes)
	}
	if !reflect.DeepEqual(manager.RuntimeSnapshot().Nodes, call.candidateState.Nodes) {
		t.Fatalf("runtime snapshot = %#v", manager.RuntimeSnapshot().Nodes)
	}
}

func TestRuntimeManagedMutationFailureLeavesBothSnapshotsUnchanged(t *testing.T) {
	wantErr := errors.New("sing-box check failed")
	applier := &recordingDeploymentApplier{err: wantErr}
	manager := newRuntimeManagedTestManager(t, applier)

	_, err := manager.Create(CreateInput{
		ID: "edge-vmess", Protocol: domain.ProtocolVMess, Port: 25555, Enabled: true,
		Deployment: runtimeconfig.PersistedNodeDeployment{
			NodeID: "edge-vmess", Listeners: []netip.Addr{netip.MustParseAddr("2001:db8::10")},
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Create() error = %v, want deployment error", err)
	}
	if len(manager.Snapshot().Nodes) != 0 || len(manager.RuntimeSnapshot().Nodes) != 0 {
		t.Fatal("failed deployment changed config or runtime snapshot")
	}
}

func TestRuntimeManagedCreateRequiresMatchingDeployment(t *testing.T) {
	applier := &recordingDeploymentApplier{}
	manager := newRuntimeManagedTestManager(t, applier)

	for _, input := range []CreateInput{
		{ID: "missing", Protocol: domain.ProtocolVLESS, Port: 24443, Enabled: true},
		{
			ID: "mismatch", Protocol: domain.ProtocolVLESS, Port: 24444, Enabled: true,
			Deployment: runtimeconfig.PersistedNodeDeployment{
				NodeID: "other", Listeners: []netip.Addr{netip.MustParseAddr("198.51.100.11")},
			},
		},
	} {
		if _, err := manager.Create(input); err == nil {
			t.Fatalf("Create(%#v) accepted an invalid deployment binding", input)
		}
	}
	if len(applier.calls) != 0 {
		t.Fatalf("invalid deployment reached applier: %#v", applier.calls)
	}
}

func TestRuntimeManagedDeleteRemovesDeploymentInSameCommit(t *testing.T) {
	applier := &recordingDeploymentApplier{}
	manager := newRuntimeManagedTestManager(t, applier)
	_, err := manager.Create(CreateInput{
		ID: "edge-socks", Protocol: domain.ProtocolSOCKS5, Port: 26666, Enabled: true,
		Deployment: runtimeconfig.PersistedNodeDeployment{
			NodeID: "edge-socks", Listeners: []netip.Addr{netip.MustParseAddr("198.51.100.12")},
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := manager.Delete("edge-socks"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if len(applier.calls) != 2 {
		t.Fatalf("deployment calls = %d, want 2", len(applier.calls))
	}
	deleted := applier.calls[1]
	if len(deleted.candidateConfig.Nodes) != 0 || len(deleted.candidateState.Nodes) != 0 {
		t.Fatalf("delete candidate = %#v / %#v", deleted.candidateConfig.Nodes, deleted.candidateState.Nodes)
	}
}

func TestRuntimeManagedMutationPreservesProtectedRemoteOutbounds(t *testing.T) {
	applier := &recordingDeploymentApplier{}
	manager := newRuntimeManagedTestManager(t, applier)
	firstSnapshot := manager.RuntimeSnapshot()
	if len(firstSnapshot.RemoteOutbounds) != 1 {
		t.Fatalf("remote outbounds = %#v", firstSnapshot.RemoteOutbounds)
	}
	firstSnapshot.RemoteOutbounds[0].Config[0] = 'x'
	if manager.RuntimeSnapshot().RemoteOutbounds[0].Config[0] == 'x' {
		t.Fatal("RuntimeSnapshot() exposed mutable remote credential storage")
	}

	_, err := manager.Create(CreateInput{
		ID: "preserve-remote", Protocol: domain.ProtocolVLESS, Port: 27777, Enabled: true,
		Deployment: runtimeconfig.PersistedNodeDeployment{
			NodeID: "preserve-remote", Listeners: []netip.Addr{netip.MustParseAddr("2001:db8::20")},
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	call := applier.calls[0]
	if len(call.currentState.RemoteOutbounds) != 1 || len(call.candidateState.RemoteOutbounds) != 1 {
		t.Fatalf("remote outbounds were lost across mutation: %#v / %#v", call.currentState, call.candidateState)
	}
}

func newRuntimeManagedTestManager(t *testing.T, applier DeploymentApplier) *Manager {
	t.Helper()
	state := runtimeconfig.DeploymentState{
		SchemaVersion: runtimeconfig.DeploymentStateSchemaVersion,
		Nodes:         []runtimeconfig.PersistedNodeDeployment{},
		IPv6Outbounds: []netip.Addr{netip.MustParseAddr("2001:db8:ffff::10")},
		RemoteOutbounds: []runtimeconfig.PersistedRemoteOutbound{{
			Config: json.RawMessage(`{"type":"vless","tag":"remote-preserved","server":"proxy.example.com","server_port":443,"uuid":"550e8400-e29b-41d4-a716-446655440000"}`),
		}},
	}
	manager, err := NewManager(ManagerOptions{
		Config:            domain.DefaultConfig(),
		Store:             &recordingStore{},
		Entropy:           bytes.NewReader(bytes.Repeat([]byte{0x52}, 1024)),
		AllocatePort:      func() (int, error) { return 24443, nil },
		RuntimeState:      &state,
		DeploymentApplier: applier,
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager
}

type deploymentApplyCall struct {
	currentConfig   domain.Config
	candidateConfig domain.Config
	currentState    runtimeconfig.DeploymentState
	candidateState  runtimeconfig.DeploymentState
}

type recordingDeploymentApplier struct {
	calls []deploymentApplyCall
	err   error
}

func (applier *recordingDeploymentApplier) Apply(currentConfig domain.Config, candidateConfig domain.Config, currentState runtimeconfig.DeploymentState, candidateState runtimeconfig.DeploymentState) error {
	applier.calls = append(applier.calls, deploymentApplyCall{
		currentConfig:   cloneTestConfig(currentConfig),
		candidateConfig: cloneTestConfig(candidateConfig),
		currentState:    cloneTestRuntimeState(currentState),
		candidateState:  cloneTestRuntimeState(candidateState),
	})
	return applier.err
}

func cloneTestRuntimeState(state runtimeconfig.DeploymentState) runtimeconfig.DeploymentState {
	state.Nodes = append([]runtimeconfig.PersistedNodeDeployment(nil), state.Nodes...)
	for index := range state.Nodes {
		state.Nodes[index].Listeners = append([]netip.Addr(nil), state.Nodes[index].Listeners...)
	}
	state.IPv6Outbounds = append([]netip.Addr(nil), state.IPv6Outbounds...)
	state.RemoteOutbounds = append([]runtimeconfig.PersistedRemoteOutbound(nil), state.RemoteOutbounds...)
	for index := range state.RemoteOutbounds {
		state.RemoteOutbounds[index].Config = append([]byte(nil), state.RemoteOutbounds[index].Config...)
	}
	state.IPv4Fallback = append([]string(nil), state.IPv4Fallback...)
	return state
}
