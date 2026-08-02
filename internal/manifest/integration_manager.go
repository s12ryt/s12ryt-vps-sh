package manifest

import (
	"context"
	"errors"
	"fmt"
	"os"

	projectnetwork "github.com/s12ryt/s12ryt-vps-sh/internal/network"
	projectsystem "github.com/s12ryt/s12ryt-vps-sh/internal/system"
)

type IntegrationRepository interface {
	Load() (Manifest, error)
	Save(Manifest) error
	Remove() error
}

type IntegrationManager struct {
	repository IntegrationRepository
	runner     projectsystem.Runner
}

type integrationCommandGroup struct {
	apply    []projectnetwork.Command
	rollback []projectnetwork.Command
}

func NewIntegrationManager(repository IntegrationRepository, runner projectsystem.Runner) (*IntegrationManager, error) {
	if repository == nil {
		return nil, errors.New("integration manifest repository is required")
	}
	if runner == nil {
		return nil, errors.New("integration command runner is required")
	}
	return &IntegrationManager{repository: repository, runner: runner}, nil
}

func (manager *IntegrationManager) Apply(ctx context.Context, candidate Manifest) error {
	if ctx == nil {
		return errors.New("integration apply context is required")
	}
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("validate integration candidate: %w", err)
	}
	if _, err := manager.repository.Load(); err == nil {
		return errors.New("integration manifest already exists; use a replacement transaction")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check current integration manifest: %w", err)
	}
	return manager.applyCandidate(ctx, candidate)
}

func (manager *IntegrationManager) Upsert(ctx context.Context, candidate Manifest) error {
	if ctx == nil {
		return errors.New("integration upsert context is required")
	}
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("validate integration upsert: %w", err)
	}
	current, err := manager.repository.Load()
	if errors.Is(err, os.ErrNotExist) {
		return manager.applyCandidate(ctx, candidate)
	}
	if err != nil {
		return fmt.Errorf("load integration manifest for upsert: %w", err)
	}
	return manager.replaceCurrent(ctx, current, candidate)
}

func (manager *IntegrationManager) applyCandidate(ctx context.Context, candidate Manifest) error {
	groups, err := buildIntegrationCommandGroups(candidate)
	if err != nil {
		return err
	}

	attempted := make([]integrationCommandGroup, 0, len(groups))
	for _, group := range groups {
		attempted = append(attempted, group)
		for _, command := range group.apply {
			if err := manager.run(ctx, command); err != nil {
				applyErr := fmt.Errorf("apply integration command: %w", err)
				return errors.Join(applyErr, manager.rollback(context.WithoutCancel(ctx), attempted))
			}
		}
	}
	if err := manager.repository.Save(candidate); err != nil {
		return errors.Join(
			fmt.Errorf("save integration manifest: %w", err),
			manager.rollback(context.WithoutCancel(ctx), attempted),
		)
	}
	return nil
}

func (manager *IntegrationManager) Replace(ctx context.Context, candidate Manifest) error {
	if ctx == nil {
		return errors.New("integration replacement context is required")
	}
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("validate integration replacement: %w", err)
	}
	current, err := manager.repository.Load()
	if err != nil {
		return fmt.Errorf("load current integration manifest: %w", err)
	}
	return manager.replaceCurrent(ctx, current, candidate)
}

func (manager *IntegrationManager) replaceCurrent(ctx context.Context, current, candidate Manifest) error {
	if err := current.Validate(); err != nil {
		return fmt.Errorf("validate current integration manifest: %w", err)
	}
	currentGroups, err := buildIntegrationCommandGroups(current)
	if err != nil {
		return fmt.Errorf("build current integration commands: %w", err)
	}
	candidateGroups, err := buildIntegrationCommandGroups(candidate)
	if err != nil {
		return fmt.Errorf("build replacement integration commands: %w", err)
	}

	cleanupContext := context.WithoutCancel(ctx)
	removedCurrent := make([]integrationCommandGroup, 0, len(currentGroups))
	for groupIndex := len(currentGroups) - 1; groupIndex >= 0; groupIndex-- {
		group := currentGroups[groupIndex]
		removedCurrent = append(removedCurrent, group)
		for _, command := range group.rollback {
			if err := manager.run(ctx, command); err != nil {
				cleanupErr := fmt.Errorf("remove current integration command: %w", err)
				return errors.Join(cleanupErr, manager.restoreRemovedGroups(cleanupContext, removedCurrent))
			}
		}
	}

	attemptedCandidate := make([]integrationCommandGroup, 0, len(candidateGroups))
	for _, group := range candidateGroups {
		attemptedCandidate = append(attemptedCandidate, group)
		for _, command := range group.apply {
			if err := manager.run(ctx, command); err != nil {
				applyErr := fmt.Errorf("apply replacement integration command: %w", err)
				return errors.Join(
					applyErr,
					manager.rollback(cleanupContext, attemptedCandidate),
					manager.applyGroups(cleanupContext, currentGroups),
				)
			}
		}
	}
	if err := manager.repository.Save(candidate); err != nil {
		saveErr := fmt.Errorf("save replacement integration manifest: %w", err)
		return errors.Join(
			saveErr,
			manager.rollback(cleanupContext, attemptedCandidate),
			manager.applyGroups(cleanupContext, currentGroups),
			manager.restoreManifest(current),
		)
	}
	return nil
}

func (manager *IntegrationManager) Restore(ctx context.Context) error {
	if ctx == nil {
		return errors.New("integration restore context is required")
	}
	current, err := manager.repository.Load()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load integration manifest for restore: %w", err)
	}
	groups, err := buildIntegrationCommandGroups(current)
	if err != nil {
		return fmt.Errorf("build integration restore commands: %w", err)
	}

	attempted := make([]integrationCommandGroup, 0, len(groups))
	for _, group := range groups {
		attempted = append(attempted, group)
		for _, command := range group.apply {
			if err := manager.run(ctx, command); err != nil {
				restoreErr := fmt.Errorf("restore integration command: %w", err)
				return errors.Join(restoreErr, manager.rollback(context.WithoutCancel(ctx), attempted))
			}
		}
	}
	return nil
}

func (manager *IntegrationManager) Remove(ctx context.Context) error {
	if ctx == nil {
		return errors.New("integration cleanup context is required")
	}
	current, err := manager.repository.Load()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load integration manifest for cleanup: %w", err)
	}
	plan, err := current.CleanupPlan()
	if err != nil {
		return fmt.Errorf("build integration cleanup: %w", err)
	}

	cleanupContext := context.WithoutCancel(ctx)
	var cleanupErrors []error
	for _, commands := range [][]projectnetwork.Command{plan.Firewall, plan.Routes, plan.Addresses} {
		for _, command := range commands {
			if err := manager.run(cleanupContext, command); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("cleanup integration command: %w", err))
			}
		}
	}
	if cleanupErr := errors.Join(cleanupErrors...); cleanupErr != nil {
		return cleanupErr
	}
	if err := manager.repository.Remove(); err != nil {
		return fmt.Errorf("remove integration manifest: %w", err)
	}
	return nil
}

func (manager *IntegrationManager) rollback(ctx context.Context, groups []integrationCommandGroup) error {
	var rollbackErrors []error
	for groupIndex := len(groups) - 1; groupIndex >= 0; groupIndex-- {
		for _, command := range groups[groupIndex].rollback {
			if err := manager.run(ctx, command); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("rollback integration command: %w", err))
			}
		}
	}
	return errors.Join(rollbackErrors...)
}

func (manager *IntegrationManager) restoreRemovedGroups(ctx context.Context, groups []integrationCommandGroup) error {
	var restoreErrors []error
	for groupIndex := len(groups) - 1; groupIndex >= 0; groupIndex-- {
		for _, command := range groups[groupIndex].apply {
			if err := manager.run(ctx, command); err != nil {
				restoreErrors = append(restoreErrors, fmt.Errorf("restore current integration command: %w", err))
			}
		}
	}
	return errors.Join(restoreErrors...)
}

func (manager *IntegrationManager) applyGroups(ctx context.Context, groups []integrationCommandGroup) error {
	var applyErrors []error
	for _, group := range groups {
		for _, command := range group.apply {
			if err := manager.run(ctx, command); err != nil {
				applyErrors = append(applyErrors, fmt.Errorf("restore current integration command: %w", err))
			}
		}
	}
	return errors.Join(applyErrors...)
}

func (manager *IntegrationManager) restoreManifest(current Manifest) error {
	if err := manager.repository.Save(current); err != nil {
		return fmt.Errorf("restore current integration manifest: %w", err)
	}
	return nil
}

func (manager *IntegrationManager) run(ctx context.Context, command projectnetwork.Command) error {
	return manager.runner.Run(ctx, projectsystem.Command{
		Name: command.Name,
		Args: append([]string(nil), command.Args...),
	})
}

func buildIntegrationCommandGroups(value Manifest) ([]integrationCommandGroup, error) {
	prefix, gateway, addresses, err := value.networkValues()
	if err != nil {
		return nil, err
	}
	addressPlan := projectnetwork.AddressPlan{
		Interface: value.Interface,
		Prefix:    prefix,
		Gateway:   gateway,
		Addresses: addresses,
	}
	addressApply, err := projectnetwork.BuildIPv6AddressCommands(addressPlan)
	if err != nil {
		return nil, fmt.Errorf("build IPv6 address apply commands: %w", err)
	}
	routes, err := projectnetwork.BuildPolicyRoutePlan(projectnetwork.PolicyRouteInput{
		Interface: value.Interface,
		Gateway:   gateway.String(),
		Addresses: append([]string(nil), value.Addresses...),
	})
	if err != nil {
		return nil, fmt.Errorf("build policy route apply commands: %w", err)
	}
	firewall, err := projectnetwork.BuildFirewallPlan(value.firewallInput())
	if err != nil {
		return nil, fmt.Errorf("build firewall apply commands: %w", err)
	}
	return []integrationCommandGroup{
		{apply: cloneCommands(addressApply), rollback: cloneCommands(projectnetwork.BuildIPv6RemovalCommands(addressPlan))},
		{apply: cloneCommands(routes.Apply), rollback: cloneCommands(routes.Remove)},
		{apply: cloneCommands(firewall.Apply), rollback: cloneCommands(firewall.Remove)},
	}, nil
}
