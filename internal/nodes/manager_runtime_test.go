package nodes

import (
	"bytes"
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

func newRuntimeManagedTestManager(t *testing.T, applier DeploymentApplier) *Manager {
	t.Helper()
	state := runtimeconfig.DeploymentState{
		SchemaVersion: runtimeconfig.DeploymentStateSchemaVersion,
		Nodes:         []runtimeconfig.PersistedNodeDeployment{},
		IPv6Outbounds: []netip.Addr{},
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
	return state
}
