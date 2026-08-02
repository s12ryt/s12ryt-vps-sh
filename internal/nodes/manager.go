package nodes

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

type ConfigStore interface {
	Save(domain.Config) error
}

type ManagerOptions struct {
	Config       domain.Config
	Store        ConfigStore
	Entropy      io.Reader
	AllocatePort func() (int, error)
}

type CreateInput struct {
	ID       string
	Protocol domain.Protocol
	Port     int
	Enabled  bool
}

type UpdateInput struct {
	ID      string
	Port    int
	Enabled bool
}

type Manager struct {
	mu           sync.RWMutex
	config       domain.Config
	store        ConfigStore
	entropy      io.Reader
	allocatePort func() (int, error)
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
	return &Manager{
		config:       cloneConfig(options.Config),
		store:        options.Store,
		entropy:      options.Entropy,
		allocatePort: options.AllocatePort,
	}, nil
}

func (manager *Manager) Snapshot() domain.Config {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return cloneConfig(manager.config)
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
	if err := manager.persist(candidate); err != nil {
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
		if err := manager.persist(candidate); err != nil {
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
		return manager.persist(candidate)
	}
	return fmt.Errorf("node %q does not exist", id)
}

func (manager *Manager) persist(candidate domain.Config) error {
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("validate candidate configuration: %w", err)
	}
	if err := manager.store.Save(candidate); err != nil {
		return fmt.Errorf("save candidate configuration: %w", err)
	}
	manager.config = cloneConfig(candidate)
	return nil
}

func cloneConfig(config domain.Config) domain.Config {
	config.Panel.AllowedCIDRs = append([]string(nil), config.Panel.AllowedCIDRs...)
	config.Nodes = append([]domain.Node(nil), config.Nodes...)
	return config
}
