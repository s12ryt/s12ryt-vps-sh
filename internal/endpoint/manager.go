package endpoint

import (
	"context"
	"errors"
	"fmt"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

type Repository interface {
	Load() (domain.Config, error)
	Save(domain.Config) error
}

type Runtime interface {
	Restart(context.Context, domain.PanelConfig) error
	Healthy(context.Context, domain.PanelConfig) error
}

type Network interface {
	ReplacePanel(context.Context, domain.PanelConfig, domain.PanelConfig) error
}

type ManagerOptions struct {
	Repository Repository
	Runtime    Runtime
	Network    Network
}

type Manager struct {
	repository Repository
	runtime    Runtime
	network    Network
}

func NewManager(options ManagerOptions) (*Manager, error) {
	if options.Repository == nil {
		return nil, errors.New("endpoint configuration repository is required")
	}
	if options.Runtime == nil {
		return nil, errors.New("endpoint runtime is required")
	}
	if options.Network == nil {
		return nil, errors.New("endpoint network replacement is required")
	}
	return &Manager{
		repository: options.Repository,
		runtime:    options.Runtime,
		network:    options.Network,
	}, nil
}

func (manager *Manager) Apply(ctx context.Context, panel domain.PanelConfig) error {
	if ctx == nil {
		return errors.New("endpoint update context is required")
	}
	current, err := manager.repository.Load()
	if err != nil {
		return fmt.Errorf("load current endpoint configuration: %w", err)
	}
	if err := current.Validate(); err != nil {
		return fmt.Errorf("validate current endpoint configuration: %w", err)
	}

	candidate := cloneConfig(current)
	candidate.Panel = clonePanel(panel)
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("validate candidate endpoint configuration: %w", err)
	}
	if err := manager.repository.Save(candidate); err != nil {
		return fmt.Errorf("save candidate endpoint configuration: %w", err)
	}

	if err := manager.runtime.Restart(ctx, candidate.Panel); err != nil {
		return errors.Join(
			fmt.Errorf("restart candidate endpoint: %w", err),
			manager.restore(context.WithoutCancel(ctx), current),
		)
	}
	if err := manager.runtime.Healthy(ctx, candidate.Panel); err != nil {
		return errors.Join(
			fmt.Errorf("check candidate endpoint health: %w", err),
			manager.restore(context.WithoutCancel(ctx), current),
		)
	}
	if err := manager.network.ReplacePanel(ctx, current.Panel, candidate.Panel); err != nil {
		return errors.Join(
			fmt.Errorf("replace endpoint firewall rules: %w", err),
			manager.restore(context.WithoutCancel(ctx), current),
		)
	}
	return nil
}

func (manager *Manager) restore(ctx context.Context, current domain.Config) error {
	if err := manager.repository.Save(current); err != nil {
		return fmt.Errorf("restore endpoint configuration: %w", err)
	}
	var restoreErrors []error
	if err := manager.runtime.Restart(ctx, current.Panel); err != nil {
		restoreErrors = append(restoreErrors, fmt.Errorf("restart restored endpoint: %w", err))
		return errors.Join(restoreErrors...)
	}
	if err := manager.runtime.Healthy(ctx, current.Panel); err != nil {
		restoreErrors = append(restoreErrors, fmt.Errorf("check restored endpoint health: %w", err))
	}
	return errors.Join(restoreErrors...)
}

func cloneConfig(config domain.Config) domain.Config {
	result := config
	result.Panel = clonePanel(config.Panel)
	result.Nodes = append([]domain.Node(nil), config.Nodes...)
	return result
}

func clonePanel(panel domain.PanelConfig) domain.PanelConfig {
	result := panel
	result.AllowedCIDRs = append([]string(nil), panel.AllowedCIDRs...)
	return result
}
