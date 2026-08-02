package nodes

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"reflect"
	"sync"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/runtimeconfig"
)

type ConfigStore interface {
	Save(domain.Config) error
}

type DeploymentApplier interface {
	Apply(currentConfig domain.Config, candidateConfig domain.Config, currentState runtimeconfig.DeploymentState, candidateState runtimeconfig.DeploymentState) error
}

type ManagerOptions struct {
	Config            domain.Config
	Store             ConfigStore
	Entropy           io.Reader
	AllocatePort      func() (int, error)
	RuntimeState      *runtimeconfig.DeploymentState
	DeploymentApplier DeploymentApplier
}

type CreateInput struct {
	ID         string
	Protocol   domain.Protocol
	Port       int
	Enabled    bool
	Deployment runtimeconfig.PersistedNodeDeployment
}

type UpdateInput struct {
	ID      string
	Port    int
	Enabled bool
}

type Manager struct {
	mu                sync.RWMutex
	config            domain.Config
	store             ConfigStore
	entropy           io.Reader
	allocatePort      func() (int, error)
	runtimeState      runtimeconfig.DeploymentState
	deploymentApplier DeploymentApplier
}

func NewManager(options ManagerOptions) (*Manager, error) {
	if options.Store == nil {
		return nil, errors.New("configuration store is required")
	}
	if options.Entropy == nil {
		return nil, errors.New("credential entropy is required")
	}
	if options.AllocatePort == nil {
		return nil, errors.New("port allocator is required")
	}
	if err := options.Config.Validate(); err != nil {
		return nil, fmt.Errorf("validate initial configuration: %w", err)
	}
	managedRuntime := options.RuntimeState != nil || options.DeploymentApplier != nil
	if managedRuntime && (options.RuntimeState == nil || options.DeploymentApplier == nil) {
		return nil, errors.New("runtime state and deployment applier must be configured together")
	}
	runtimeState := runtimeconfig.DeploymentState{}
	if managedRuntime {
		runtimeState = cloneRuntimeState(*options.RuntimeState)
		if _, err := runtimeState.Resolve(options.Config); err != nil {
			return nil, fmt.Errorf("validate initial runtime state: %w", err)
		}
	}
	return &Manager{
		config:            cloneConfig(options.Config),
		store:             options.Store,
		entropy:           options.Entropy,
		allocatePort:      options.AllocatePort,
		runtimeState:      runtimeState,
		deploymentApplier: options.DeploymentApplier,
	}, nil
}

func (manager *Manager) Snapshot() domain.Config {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return cloneConfig(manager.config)
}

func (manager *Manager) RuntimeSnapshot() runtimeconfig.DeploymentState {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return cloneRuntimeState(manager.runtimeState)
}

func (manager *Manager) ReplaceConfig(candidate domain.Config) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if !reflect.DeepEqual(candidate.Nodes, manager.config.Nodes) {
		return errors.New("managed nodes must be changed through node operations")
	}
	return manager.persist(cloneConfig(candidate), cloneRuntimeState(manager.runtimeState))
}

func (manager *Manager) Create(input CreateInput) (domain.Node, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	for _, existing := range manager.config.Nodes {
		if existing.ID == input.ID {
			return domain.Node{}, fmt.Errorf("node %q already exists", input.ID)
		}
	}
	port := input.Port
	if port == 0 {
		allocated, err := manager.allocatePort()
		if err != nil {
			return domain.Node{}, fmt.Errorf("allocate node port: %w", err)
		}
		port = allocated
	}
	credential, err := domain.GenerateNodeCredential(input.Protocol, manager.entropy)
	if err != nil {
		return domain.Node{}, fmt.Errorf("generate node credential: %w", err)
	}
	node := domain.Node{
		ID:         input.ID,
		Protocol:   input.Protocol,
		Port:       port,
		Enabled:    input.Enabled,
		Credential: credential,
	}
	candidate := cloneConfig(manager.config)
	candidate.Nodes = append(candidate.Nodes, node)
	candidateRuntime := cloneRuntimeState(manager.runtimeState)
	if manager.deploymentApplier != nil {
		if input.Deployment.NodeID == "" {
			return domain.Node{}, errors.New("runtime-managed nodes require deployment data")
		}
		if input.Deployment.NodeID != input.ID {
			return domain.Node{}, errors.New("deployment node ID must match the managed node ID")
		}
		candidateRuntime.Nodes = append(candidateRuntime.Nodes, cloneRuntimeNode(input.Deployment))
	}
	if err := manager.persist(candidate, candidateRuntime); err != nil {
		return domain.Node{}, err
	}
	return node, nil
}

func (manager *Manager) Update(input UpdateInput) (domain.Node, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	candidate := cloneConfig(manager.config)
	for index := range candidate.Nodes {
		if candidate.Nodes[index].ID != input.ID {
			continue
		}
		candidate.Nodes[index].Port = input.Port
		candidate.Nodes[index].Enabled = input.Enabled
		if err := manager.persist(candidate, cloneRuntimeState(manager.runtimeState)); err != nil {
			return domain.Node{}, err
		}
		return candidate.Nodes[index], nil
	}
	return domain.Node{}, fmt.Errorf("node %q does not exist", input.ID)
}

func (manager *Manager) Delete(id string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	candidate := cloneConfig(manager.config)
	for index := range candidate.Nodes {
		if candidate.Nodes[index].ID != id {
			continue
		}
		candidate.Nodes = append(candidate.Nodes[:index], candidate.Nodes[index+1:]...)
		candidateRuntime := cloneRuntimeState(manager.runtimeState)
		if manager.deploymentApplier != nil {
			for runtimeIndex := range candidateRuntime.Nodes {
				if candidateRuntime.Nodes[runtimeIndex].NodeID != id {
					continue
				}
				candidateRuntime.Nodes = append(candidateRuntime.Nodes[:runtimeIndex], candidateRuntime.Nodes[runtimeIndex+1:]...)
				break
			}
		}
		return manager.persist(candidate, candidateRuntime)
	}
	return fmt.Errorf("node %q does not exist", id)
}

func (manager *Manager) persist(candidate domain.Config, candidateRuntime runtimeconfig.DeploymentState) error {
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("validate candidate configuration: %w", err)
	}
	if manager.deploymentApplier != nil {
		if _, err := candidateRuntime.Resolve(candidate); err != nil {
			return fmt.Errorf("validate candidate runtime state: %w", err)
		}
		if err := manager.deploymentApplier.Apply(
			cloneConfig(manager.config),
			cloneConfig(candidate),
			cloneRuntimeState(manager.runtimeState),
			cloneRuntimeState(candidateRuntime),
		); err != nil {
			return fmt.Errorf("apply candidate deployment: %w", err)
		}
		manager.config = cloneConfig(candidate)
		manager.runtimeState = cloneRuntimeState(candidateRuntime)
		return nil
	}
	if err := manager.store.Save(candidate); err != nil {
		return fmt.Errorf("save candidate configuration: %w", err)
	}
	manager.config = cloneConfig(candidate)
	return nil
}

func cloneRuntimeState(state runtimeconfig.DeploymentState) runtimeconfig.DeploymentState {
	state.Nodes = append([]runtimeconfig.PersistedNodeDeployment(nil), state.Nodes...)
	for index := range state.Nodes {
		state.Nodes[index] = cloneRuntimeNode(state.Nodes[index])
	}
	state.IPv6Outbounds = append([]netip.Addr(nil), state.IPv6Outbounds...)
	state.RemoteOutbounds = append([]runtimeconfig.PersistedRemoteOutbound(nil), state.RemoteOutbounds...)
	for index := range state.RemoteOutbounds {
		state.RemoteOutbounds[index].Config = append([]byte(nil), state.RemoteOutbounds[index].Config...)
	}
	state.IPv4Fallback = append([]string(nil), state.IPv4Fallback...)
	return state
}

func cloneRuntimeNode(node runtimeconfig.PersistedNodeDeployment) runtimeconfig.PersistedNodeDeployment {
	node.Listeners = append([]netip.Addr(nil), node.Listeners...)
	if node.TLS.Reality != nil {
		reality := *node.TLS.Reality
		node.TLS.Reality = &reality
	}
	return node
}

func cloneConfig(config domain.Config) domain.Config {
	config.Panel.AllowedCIDRs = append([]string(nil), config.Panel.AllowedCIDRs...)
	config.Nodes = append([]domain.Node(nil), config.Nodes...)
	return config
}
