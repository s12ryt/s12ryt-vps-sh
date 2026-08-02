package endpoint

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

func TestManagerAppliesEndpointAfterCandidateHealthCheck(t *testing.T) {
	events := make([]string, 0, 5)
	repository := &recordingRepository{current: domain.DefaultConfig(), events: &events}
	runtime := &recordingRuntime{events: &events}
	network := &recordingNetwork{events: &events}
	manager, err := NewManager(ManagerOptions{Repository: repository, Runtime: runtime, Network: network})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	candidate := domain.PanelConfig{
		Port:         35555,
		Path:         "/newpanel1234",
		ListenIPv6:   "2001:db8:100::20",
		AllowedCIDRs: []string{"0.0.0.0/0", "::/0"},
	}
	if err := manager.Apply(context.Background(), candidate); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	wantEvents := []string{
		"load:34456:/configureme1:",
		"save:35555:/newpanel1234:2001:db8:100::20",
		"restart:35555:/newpanel1234:2001:db8:100::20",
		"health:35555:/newpanel1234:2001:db8:100::20",
		"network:34456:35555",
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", events, wantEvents)
	}
	if !reflect.DeepEqual(repository.current.Panel, candidate) {
		t.Fatalf("stored panel = %#v, want %#v", repository.current.Panel, candidate)
	}
}

func TestManagerRestoresOldEndpointWhenCandidateHealthFails(t *testing.T) {
	healthErr := errors.New("candidate health failed")
	events := make([]string, 0, 7)
	repository := &recordingRepository{current: domain.DefaultConfig(), events: &events}
	runtime := &recordingRuntime{events: &events, healthErrors: []error{healthErr, nil}}
	network := &recordingNetwork{events: &events}
	manager, err := NewManager(ManagerOptions{Repository: repository, Runtime: runtime, Network: network})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = manager.Apply(context.Background(), domain.PanelConfig{
		Port:         35555,
		Path:         "/newpanel1234",
		AllowedCIDRs: []string{"0.0.0.0/0", "::/0"},
	})
	if !errors.Is(err, healthErr) {
		t.Fatalf("Apply() error = %v, want candidate health error", err)
	}
	wantEvents := []string{
		"load:34456:/configureme1:",
		"save:35555:/newpanel1234:",
		"restart:35555:/newpanel1234:",
		"health:35555:/newpanel1234:",
		"save:34456:/configureme1:",
		"restart:34456:/configureme1:",
		"health:34456:/configureme1:",
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", events, wantEvents)
	}
	if repository.current.Panel.Port != 34456 || network.calls != 0 {
		t.Fatalf("rollback current = %#v, network calls = %d", repository.current.Panel, network.calls)
	}
}

func TestManagerRestoresOldEndpointWhenFirewallReplacementFails(t *testing.T) {
	networkErr := errors.New("firewall replacement failed")
	events := make([]string, 0, 8)
	repository := &recordingRepository{current: domain.DefaultConfig(), events: &events}
	runtime := &recordingRuntime{events: &events}
	network := &recordingNetwork{events: &events, err: networkErr}
	manager, err := NewManager(ManagerOptions{Repository: repository, Runtime: runtime, Network: network})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = manager.Apply(context.Background(), domain.PanelConfig{
		Port:         35555,
		Path:         "/newpanel1234",
		AllowedCIDRs: []string{"0.0.0.0/0", "::/0"},
	})
	if !errors.Is(err, networkErr) {
		t.Fatalf("Apply() error = %v, want network error", err)
	}
	wantEvents := []string{
		"load:34456:/configureme1:",
		"save:35555:/newpanel1234:",
		"restart:35555:/newpanel1234:",
		"health:35555:/newpanel1234:",
		"network:34456:35555",
		"save:34456:/configureme1:",
		"restart:34456:/configureme1:",
		"health:34456:/configureme1:",
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", events, wantEvents)
	}
}

func TestManagerRejectsInvalidEndpointBeforeMutation(t *testing.T) {
	events := make([]string, 0)
	repository := &recordingRepository{current: domain.DefaultConfig(), events: &events}
	runtime := &recordingRuntime{events: &events}
	network := &recordingNetwork{events: &events}
	manager, err := NewManager(ManagerOptions{Repository: repository, Runtime: runtime, Network: network})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = manager.Apply(context.Background(), domain.PanelConfig{
		Port:         0,
		Path:         "unsafe/path",
		AllowedCIDRs: []string{"0.0.0.0/0"},
	})
	if err == nil {
		t.Fatal("Apply() accepted an invalid panel endpoint")
	}
	if len(events) != 1 || events[0] != "load:34456:/configureme1:" {
		t.Fatalf("events = %#v, want only the protected load", events)
	}
}

func TestNewManagerRejectsMissingDependencies(t *testing.T) {
	repository := &recordingRepository{current: domain.DefaultConfig()}
	runtime := &recordingRuntime{}
	network := &recordingNetwork{}
	tests := map[string]ManagerOptions{
		"repository": {Runtime: runtime, Network: network},
		"runtime":    {Repository: repository, Network: network},
		"network":    {Repository: repository, Runtime: runtime},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := NewManager(options); err == nil {
				t.Fatal("NewManager() accepted a missing dependency")
			}
		})
	}
}

type recordingRepository struct {
	current domain.Config
	events  *[]string
	saveErr error
}

func (repository *recordingRepository) Load() (domain.Config, error) {
	repository.record("load", repository.current.Panel)
	return repository.current, nil
}

func (repository *recordingRepository) Save(config domain.Config) error {
	repository.record("save", config.Panel)
	if repository.saveErr != nil {
		return repository.saveErr
	}
	repository.current = config
	return nil
}

func (repository *recordingRepository) record(action string, panel domain.PanelConfig) {
	if repository.events != nil {
		*repository.events = append(*repository.events, endpointEvent(action, panel))
	}
}

type recordingRuntime struct {
	events       *[]string
	healthErrors []error
}

func (runtime *recordingRuntime) Restart(_ context.Context, panel domain.PanelConfig) error {
	if runtime.events != nil {
		*runtime.events = append(*runtime.events, endpointEvent("restart", panel))
	}
	return nil
}

func (runtime *recordingRuntime) Healthy(_ context.Context, panel domain.PanelConfig) error {
	if runtime.events != nil {
		*runtime.events = append(*runtime.events, endpointEvent("health", panel))
	}
	if len(runtime.healthErrors) == 0 {
		return nil
	}
	err := runtime.healthErrors[0]
	runtime.healthErrors = runtime.healthErrors[1:]
	return err
}

type recordingNetwork struct {
	events *[]string
	calls  int
	err    error
}

func (network *recordingNetwork) ReplacePanel(_ context.Context, current, candidate domain.PanelConfig) error {
	network.calls++
	if network.events != nil {
		*network.events = append(*network.events, "network:"+itoa(current.Port)+":"+itoa(candidate.Port))
	}
	return network.err
}

func endpointEvent(action string, panel domain.PanelConfig) string {
	return action + ":" + itoa(panel.Port) + ":" + panel.Path + ":" + panel.ListenIPv6
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	return result
}
