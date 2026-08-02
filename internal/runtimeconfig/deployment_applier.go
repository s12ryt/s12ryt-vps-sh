package runtimeconfig

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

type DeploymentConfigStore interface {
	Save(domain.Config) error
}

type DeploymentStateWriter interface {
	Save(DeploymentState) error
}

type DeploymentRuntime interface {
	Reload(context.Context) error
	Healthy(context.Context) error
}

type DeploymentApplierOptions struct {
	RuntimeConfigPath string
	ConfigStore       DeploymentConfigStore
	StateStore        DeploymentStateWriter
	Validate          func([]byte) error
	Runtime           DeploymentRuntime
}

type DeploymentApplier struct {
	runtimeConfigPath string
	configStore       DeploymentConfigStore
	stateStore        DeploymentStateWriter
	validate          func([]byte) error
	runtime           DeploymentRuntime
}

func NewDeploymentApplier(options DeploymentApplierOptions) (*DeploymentApplier, error) {
	if !filepath.IsAbs(options.RuntimeConfigPath) {
		return nil, errors.New("runtime configuration path must be absolute")
	}
	if options.ConfigStore == nil {
		return nil, errors.New("deployment configuration store is required")
	}
	if options.StateStore == nil {
		return nil, errors.New("deployment state store is required")
	}
	if options.Validate == nil {
		return nil, errors.New("runtime configuration validator is required")
	}
	if options.Runtime == nil {
		return nil, errors.New("deployment runtime is required")
	}
	return &DeploymentApplier{
		runtimeConfigPath: filepath.Clean(options.RuntimeConfigPath),
		configStore:       options.ConfigStore,
		stateStore:        options.StateStore,
		validate:          options.Validate,
		runtime:           options.Runtime,
	}, nil
}

func (applier *DeploymentApplier) Apply(
	currentConfig domain.Config,
	candidateConfig domain.Config,
	currentState DeploymentState,
	candidateState DeploymentState,
) error {
	currentPayload, err := compileDeploymentState(currentConfig, currentState)
	if err != nil {
		return fmt.Errorf("compile current runtime configuration: %w", err)
	}
	candidatePayload, err := compileDeploymentState(candidateConfig, candidateState)
	if err != nil {
		return fmt.Errorf("compile candidate runtime configuration: %w", err)
	}
	if err := applier.validate(candidatePayload); err != nil {
		return fmt.Errorf("validate candidate runtime configuration: %w", err)
	}

	ctx := context.Background()
	if err := atomicWriteRuntimeConfig(ctx, applier.runtimeConfigPath, candidatePayload); err != nil {
		return fmt.Errorf("write candidate runtime configuration: %w", err)
	}
	if err := applier.stateStore.Save(candidateState); err != nil {
		return errors.Join(
			fmt.Errorf("save candidate deployment state: %w", err),
			applier.restore(ctx, currentConfig, currentState, currentPayload, false),
		)
	}
	if err := applier.configStore.Save(candidateConfig); err != nil {
		return errors.Join(
			fmt.Errorf("save candidate configuration: %w", err),
			applier.restore(ctx, currentConfig, currentState, currentPayload, false),
		)
	}
	if err := applier.runtime.Reload(ctx); err != nil {
		return errors.Join(
			fmt.Errorf("reload candidate runtime: %w", err),
			applier.restore(ctx, currentConfig, currentState, currentPayload, true),
		)
	}
	if err := applier.runtime.Healthy(ctx); err != nil {
		return errors.Join(
			fmt.Errorf("candidate runtime health check: %w", err),
			applier.restore(ctx, currentConfig, currentState, currentPayload, true),
		)
	}
	return nil
}

func compileDeploymentState(config domain.Config, state DeploymentState) ([]byte, error) {
	input, err := state.Resolve(config)
	if err != nil {
		return nil, err
	}
	return CompileServerConfig(input)
}

func (applier *DeploymentApplier) restore(
	ctx context.Context,
	config domain.Config,
	state DeploymentState,
	payload []byte,
	reload bool,
) error {
	writeErr := atomicWriteRuntimeConfig(ctx, applier.runtimeConfigPath, payload)
	if writeErr != nil {
		writeErr = fmt.Errorf("restore previous runtime configuration: %w", writeErr)
	}
	configErr := applier.configStore.Save(config)
	if configErr != nil {
		configErr = fmt.Errorf("restore previous configuration: %w", configErr)
	}
	stateErr := applier.stateStore.Save(state)
	if stateErr != nil {
		stateErr = fmt.Errorf("restore previous deployment state: %w", stateErr)
	}
	var reloadErr error
	if reload {
		reloadErr = applier.runtime.Reload(ctx)
		if reloadErr != nil {
			reloadErr = fmt.Errorf("reload previous runtime: %w", reloadErr)
		}
	}
	return errors.Join(writeErr, configErr, stateErr, reloadErr)
}
