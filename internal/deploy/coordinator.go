package deploy

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

type Component interface {
	Prepare(context.Context, domain.Config, domain.Config) (PreparedChange, error)
}

type PreparedChange interface {
	Apply(context.Context) error
	Rollback(context.Context) error
}

type Runtime interface {
	Reload(context.Context) error
	Healthy(context.Context) error
}

type CoordinatorOptions struct {
	Repository Repository
	Components []Component
	Runtime    Runtime
}

type Coordinator struct {
	repository Repository
	components []Component
	runtime    Runtime
}

func NewCoordinator(options CoordinatorOptions) (*Coordinator, error) {
	if options.Repository == nil {
		return nil, errors.New("deployment repository is required")
	}
	if len(options.Components) == 0 {
		return nil, errors.New("at least one deployment component is required")
	}
	for index, component := range options.Components {
		if component == nil {
			return nil, fmt.Errorf("deployment component %d is nil", index)
		}
	}
	if options.Runtime == nil {
		return nil, errors.New("deployment runtime is required")
	}

	components := append([]Component(nil), options.Components...)
	return &Coordinator{
		repository: options.Repository,
		components: components,
		runtime:    options.Runtime,
	}, nil
}

func (coordinator *Coordinator) Apply(ctx context.Context, candidate domain.Config) error {
	if ctx == nil {
		return errors.New("deployment context is required")
	}
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("validate candidate configuration: %w", err)
	}

	previous, err := coordinator.repository.Load()
	if err != nil {
		return fmt.Errorf("load current configuration: %w", err)
	}
	if err := previous.Validate(); err != nil {
		return fmt.Errorf("validate current configuration: %w", err)
	}

	changes := make([]PreparedChange, 0, len(coordinator.components))
	for index, component := range coordinator.components {
		change, prepareErr := component.Prepare(ctx, previous, candidate)
		if prepareErr != nil {
			return fmt.Errorf("prepare deployment component %d: %w", index, prepareErr)
		}
		if change == nil {
			return fmt.Errorf("prepare deployment component %d returned no change", index)
		}
		changes = append(changes, change)
	}

	attempted := make([]PreparedChange, 0, len(changes))
	for index, change := range changes {
		attempted = append(attempted, change)
		if applyErr := change.Apply(ctx); applyErr != nil {
			rollbackErr := rollbackChanges(context.WithoutCancel(ctx), attempted)
			return errors.Join(fmt.Errorf("apply deployment component %d: %w", index, applyErr), rollbackErr)
		}
	}

	if err := coordinator.repository.Save(candidate); err != nil {
		rollbackErr := rollbackChanges(context.WithoutCancel(ctx), attempted)
		return errors.Join(fmt.Errorf("save candidate configuration: %w", err), rollbackErr)
	}

	if err := coordinator.runtime.Reload(ctx); err != nil {
		return coordinator.restorePrevious(ctx, previous, attempted, fmt.Errorf("reload candidate runtime: %w", err))
	}
	if err := coordinator.runtime.Healthy(ctx); err != nil {
		return coordinator.restorePrevious(ctx, previous, attempted, fmt.Errorf("candidate runtime health check: %w", err))
	}
	return nil
}

func (coordinator *Coordinator) restorePrevious(
	ctx context.Context,
	previous domain.Config,
	changes []PreparedChange,
	cause error,
) error {
	restoreContext := context.WithoutCancel(ctx)
	rollbackErr := rollbackChanges(restoreContext, changes)
	saveErr := coordinator.repository.Save(previous)
	if saveErr != nil {
		saveErr = fmt.Errorf("restore previous configuration: %w", saveErr)
	}
	reloadErr := coordinator.runtime.Reload(restoreContext)
	if reloadErr != nil {
		reloadErr = fmt.Errorf("reload previous runtime: %w", reloadErr)
	}
	return errors.Join(cause, rollbackErr, saveErr, reloadErr)
}

func rollbackChanges(ctx context.Context, changes []PreparedChange) error {
	var rollbackErrors []error
	for index := len(changes) - 1; index >= 0; index-- {
		if err := changes[index].Rollback(ctx); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback deployment component %d: %w", index, err))
		}
	}
	return errors.Join(rollbackErrors...)
}
