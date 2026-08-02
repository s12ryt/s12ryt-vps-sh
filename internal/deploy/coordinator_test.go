package deploy

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

func TestCoordinatorAppliesPreparedChangesThenPersistsReloadsAndChecksHealth(t *testing.T) {
	events := []string{}
	repository := newRecordingRepository(&events)
	runtime := &recordingRuntime{events: &events}
	coordinator, err := NewCoordinator(CoordinatorOptions{
		Repository: repository,
		Components: []Component{
			&recordingComponent{name: "network", events: &events},
			&recordingComponent{name: "firewall", events: &events},
		},
		Runtime: runtime,
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	candidate := changedConfig()

	if err := coordinator.Apply(context.Background(), candidate); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	want := []string{
		"load",
		"prepare network", "prepare firewall",
		"apply network", "apply firewall",
		"save 35555", "reload", "health",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
	if repository.current.Panel.Port != 35555 {
		t.Fatalf("persisted port = %d, want 35555", repository.current.Panel.Port)
	}
}

func TestCoordinatorRollsBackAttemptedChangesWhenApplyFails(t *testing.T) {
	events := []string{}
	applyFailure := errors.New("firewall apply failed")
	repository := newRecordingRepository(&events)
	coordinator, err := NewCoordinator(CoordinatorOptions{
		Repository: repository,
		Components: []Component{
			&recordingComponent{name: "network", events: &events},
			&recordingComponent{name: "firewall", events: &events, applyErr: applyFailure},
		},
		Runtime: &recordingRuntime{events: &events},
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	err = coordinator.Apply(context.Background(), changedConfig())
	if !errors.Is(err, applyFailure) {
		t.Fatalf("Apply() error = %v, want apply failure", err)
	}
	want := []string{
		"load", "prepare network", "prepare firewall",
		"apply network", "apply firewall",
		"rollback firewall", "rollback network",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
	if repository.current.Panel.Port != 34456 {
		t.Fatal("failed component apply persisted candidate config")
	}
}

func TestCoordinatorRestoresPreviousConfigAndRuntimeWhenHealthCheckFails(t *testing.T) {
	events := []string{}
	healthFailure := errors.New("runtime unhealthy")
	rollbackFailure := errors.New("firewall rollback failed")
	repository := newRecordingRepository(&events)
	coordinator, err := NewCoordinator(CoordinatorOptions{
		Repository: repository,
		Components: []Component{
			&recordingComponent{name: "network", events: &events},
			&recordingComponent{name: "firewall", events: &events, rollbackErr: rollbackFailure},
		},
		Runtime: &recordingRuntime{events: &events, healthErr: healthFailure},
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	err = coordinator.Apply(context.Background(), changedConfig())
	if !errors.Is(err, healthFailure) || !errors.Is(err, rollbackFailure) {
		t.Fatalf("Apply() error = %v, want health and rollback failures", err)
	}
	want := []string{
		"load", "prepare network", "prepare firewall",
		"apply network", "apply firewall", "save 35555", "reload", "health",
		"rollback firewall", "rollback network", "save 34456", "reload",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
	if repository.current.Panel.Port != 34456 {
		t.Fatalf("restored port = %d, want 34456", repository.current.Panel.Port)
	}
}

func TestCoordinatorRejectsUnsafeOptionsAndInvalidCandidateBeforeSideEffects(t *testing.T) {
	events := []string{}
	repository := newRecordingRepository(&events)
	runtime := &recordingRuntime{events: &events}
	component := &recordingComponent{name: "network", events: &events}

	tests := map[string]CoordinatorOptions{
		"missing repository": {Components: []Component{component}, Runtime: runtime},
		"missing components": {Repository: repository, Runtime: runtime},
		"nil component":      {Repository: repository, Components: []Component{nil}, Runtime: runtime},
		"missing runtime":    {Repository: repository, Components: []Component{component}},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := NewCoordinator(options); err == nil {
				t.Fatal("NewCoordinator() succeeded")
			}
		})
	}

	coordinator, err := NewCoordinator(CoordinatorOptions{
		Repository: repository,
		Components: []Component{component},
		Runtime:    runtime,
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	invalid := changedConfig()
	invalid.Panel.Port = 0
	if err := coordinator.Apply(context.Background(), invalid); err == nil {
		t.Fatal("Apply() accepted invalid candidate")
	}
	if len(events) != 0 {
		t.Fatalf("invalid candidate caused side effects: %#v", events)
	}
}

type recordingRepository struct {
	events  *[]string
	current domain.Config
}

func newRecordingRepository(events *[]string) *recordingRepository {
	return &recordingRepository{events: events, current: domain.DefaultConfig()}
}

func (repository *recordingRepository) Load() (domain.Config, error) {
	*repository.events = append(*repository.events, "load")
	return repository.current, nil
}

func (repository *recordingRepository) Save(config domain.Config) error {
	*repository.events = append(*repository.events, "save "+strconv.Itoa(config.Panel.Port))
	repository.current = config
	return nil
}

type recordingComponent struct {
	name        string
	events      *[]string
	prepareErr  error
	applyErr    error
	rollbackErr error
}

func (component *recordingComponent) Prepare(_ context.Context, _ domain.Config, _ domain.Config) (PreparedChange, error) {
	*component.events = append(*component.events, "prepare "+component.name)
	if component.prepareErr != nil {
		return nil, component.prepareErr
	}
	return &recordingChange{component: component}, nil
}

type recordingChange struct {
	component *recordingComponent
}

func (change *recordingChange) Apply(context.Context) error {
	*change.component.events = append(*change.component.events, "apply "+change.component.name)
	return change.component.applyErr
}

func (change *recordingChange) Rollback(context.Context) error {
	*change.component.events = append(*change.component.events, "rollback "+change.component.name)
	return change.component.rollbackErr
}

type recordingRuntime struct {
	events    *[]string
	reloadErr error
	healthErr error
}

func (runtime *recordingRuntime) Reload(context.Context) error {
	*runtime.events = append(*runtime.events, "reload")
	return runtime.reloadErr
}

func (runtime *recordingRuntime) Healthy(context.Context) error {
	*runtime.events = append(*runtime.events, "health")
	return runtime.healthErr
}

func changedConfig() domain.Config {
	config := domain.DefaultConfig()
	config.Panel.Port = 35555
	return config
}
