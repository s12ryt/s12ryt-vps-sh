package runtimeconfig

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

func TestDeploymentApplierCommitsRuntimeStateConfigurationAndHealthyRuntime(t *testing.T) {
	currentConfig, candidateConfig, currentState, candidateState := deploymentSnapshots()
	events := []string{}
	configStore := &deploymentConfigStore{current: currentConfig, events: &events}
	stateStore := &deploymentStateRecorder{current: currentState, events: &events}
	runtime := &deploymentRuntime{events: &events}
	runtimePath := filepath.Join(t.TempDir(), "sing-box.json")
	currentPayload, err := compileDeploymentPayload(currentConfig, currentState)
	if err != nil {
		t.Fatalf("compile current payload: %v", err)
	}
	if err := os.WriteFile(runtimePath, currentPayload, 0o600); err != nil {
		t.Fatalf("write current runtime: %v", err)
	}

	applier, err := NewDeploymentApplier(DeploymentApplierOptions{
		RuntimeConfigPath: runtimePath,
		ConfigStore:       configStore,
		StateStore:        stateStore,
		Validate: func(payload []byte) error {
			events = append(events, "validate")
			if !strings.Contains(string(payload), `"in-edge-vless-v6"`) {
				t.Fatalf("candidate runtime payload = %s", payload)
			}
			return nil
		},
		Runtime: runtime,
	})
	if err != nil {
		t.Fatalf("NewDeploymentApplier() error = %v", err)
	}
	if err := applier.Apply(currentConfig, candidateConfig, currentState, candidateState); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	wantEvents := []string{"validate", "state:1", "config:1", "reload", "health"}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", events, wantEvents)
	}
	if !reflect.DeepEqual(configStore.current, candidateConfig) || !reflect.DeepEqual(stateStore.current, candidateState) {
		t.Fatal("candidate config and runtime state were not committed together")
	}
	storedPayload, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatalf("read candidate runtime: %v", err)
	}
	if !strings.Contains(string(storedPayload), `"in-edge-vless-v6"`) {
		t.Fatalf("stored candidate runtime = %s", storedPayload)
	}
	info, err := os.Stat(runtimePath)
	if err != nil {
		t.Fatalf("stat runtime config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("runtime config mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestDeploymentApplierValidationFailureHasNoSideEffects(t *testing.T) {
	currentConfig, candidateConfig, currentState, candidateState := deploymentSnapshots()
	wantErr := errors.New("sing-box check failed")
	events := []string{}
	runtimePath := filepath.Join(t.TempDir(), "sing-box.json")
	if err := os.WriteFile(runtimePath, []byte("old-runtime\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	applier, err := NewDeploymentApplier(DeploymentApplierOptions{
		RuntimeConfigPath: runtimePath,
		ConfigStore:       &deploymentConfigStore{current: currentConfig, events: &events},
		StateStore:        &deploymentStateRecorder{current: currentState, events: &events},
		Validate:          func([]byte) error { return wantErr },
		Runtime:           &deploymentRuntime{events: &events},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = applier.Apply(currentConfig, candidateConfig, currentState, candidateState)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Apply() error = %v, want validation error", err)
	}
	if len(events) != 0 {
		t.Fatalf("validation failure side effects = %#v", events)
	}
	contents, _ := os.ReadFile(runtimePath)
	if string(contents) != "old-runtime\n" {
		t.Fatalf("validation failure changed runtime file: %q", contents)
	}
}

func TestDeploymentApplierHealthFailureRestoresAllPreviousState(t *testing.T) {
	currentConfig, candidateConfig, currentState, candidateState := deploymentSnapshots()
	healthErr := errors.New("runtime unhealthy")
	events := []string{}
	configStore := &deploymentConfigStore{current: currentConfig, events: &events}
	stateStore := &deploymentStateRecorder{current: currentState, events: &events}
	runtime := &deploymentRuntime{events: &events, healthErr: healthErr}
	runtimePath := filepath.Join(t.TempDir(), "sing-box.json")
	currentPayload, err := compileDeploymentPayload(currentConfig, currentState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimePath, currentPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	applier, err := NewDeploymentApplier(DeploymentApplierOptions{
		RuntimeConfigPath: runtimePath,
		ConfigStore:       configStore,
		StateStore:        stateStore,
		Validate:          func([]byte) error { events = append(events, "validate"); return nil },
		Runtime:           runtime,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = applier.Apply(currentConfig, candidateConfig, currentState, candidateState)
	if !errors.Is(err, healthErr) {
		t.Fatalf("Apply() error = %v, want health error", err)
	}
	wantEvents := []string{
		"validate", "state:1", "config:1", "reload", "health",
		"config:0", "state:0", "reload",
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("rollback events = %#v, want %#v", events, wantEvents)
	}
	if !reflect.DeepEqual(configStore.current, currentConfig) || !reflect.DeepEqual(stateStore.current, currentState) {
		t.Fatal("health failure did not restore previous stores")
	}
	restoredPayload, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restoredPayload, currentPayload) {
		t.Fatalf("restored runtime = %s, want %s", restoredPayload, currentPayload)
	}
}

func TestNewDeploymentApplierRejectsUnsafeOptions(t *testing.T) {
	valid := DeploymentApplierOptions{
		RuntimeConfigPath: "/opt/s12ryt-ipv6/config/sing-box.json",
		ConfigStore:       &deploymentConfigStore{},
		StateStore:        &deploymentStateRecorder{},
		Validate:          func([]byte) error { return nil },
		Runtime:           &deploymentRuntime{},
	}
	mutations := []func(*DeploymentApplierOptions){
		func(options *DeploymentApplierOptions) { options.RuntimeConfigPath = "relative.json" },
		func(options *DeploymentApplierOptions) { options.ConfigStore = nil },
		func(options *DeploymentApplierOptions) { options.StateStore = nil },
		func(options *DeploymentApplierOptions) { options.Validate = nil },
		func(options *DeploymentApplierOptions) { options.Runtime = nil },
	}
	for _, mutate := range mutations {
		options := valid
		mutate(&options)
		if _, err := NewDeploymentApplier(options); err == nil {
			t.Fatalf("NewDeploymentApplier(%#v) accepted unsafe options", options)
		}
	}
}

func deploymentSnapshots() (domain.Config, domain.Config, DeploymentState, DeploymentState) {
	currentConfig := domain.DefaultConfig()
	currentState := DeploymentState{
		SchemaVersion: DeploymentStateSchemaVersion,
		Nodes:         []PersistedNodeDeployment{},
		IPv6Outbounds: []netip.Addr{},
	}
	candidateConfig := currentConfig
	candidateConfig.Nodes = []domain.Node{{
		ID: "edge-vless", Protocol: domain.ProtocolVLESS, Port: 24443, Enabled: true,
		Credential: domain.NodeCredential{UUID: "123e4567-e89b-42d3-a456-426614174000"},
	}}
	candidateState := currentState
	candidateState.Nodes = []PersistedNodeDeployment{{
		NodeID: "edge-vless", Listeners: []netip.Addr{netip.MustParseAddr("2001:db8::10")},
	}}
	return currentConfig, candidateConfig, currentState, candidateState
}

func compileDeploymentPayload(config domain.Config, state DeploymentState) ([]byte, error) {
	input, err := state.Resolve(config)
	if err != nil {
		return nil, err
	}
	return CompileServerConfig(input)
}

type deploymentConfigStore struct {
	current domain.Config
	events  *[]string
}

func (store *deploymentConfigStore) Save(config domain.Config) error {
	store.current = config
	*store.events = append(*store.events, "config:"+deploymentNodeCount(config.Nodes))
	return nil
}

type deploymentStateRecorder struct {
	current DeploymentState
	events  *[]string
}

func (store *deploymentStateRecorder) Save(state DeploymentState) error {
	store.current = state
	*store.events = append(*store.events, "state:"+deploymentNodeCount(state.Nodes))
	return nil
}

type deploymentRuntime struct {
	events    *[]string
	healthErr error
}

func (runtime *deploymentRuntime) Reload(context.Context) error {
	*runtime.events = append(*runtime.events, "reload")
	return nil
}

func (runtime *deploymentRuntime) Healthy(context.Context) error {
	*runtime.events = append(*runtime.events, "health")
	return runtime.healthErr
}

func deploymentNodeCount(nodes any) string {
	switch typed := nodes.(type) {
	case []domain.Node:
		if len(typed) == 0 {
			return "0"
		}
	case []PersistedNodeDeployment:
		if len(typed) == 0 {
			return "0"
		}
	}
	return "1"
}
