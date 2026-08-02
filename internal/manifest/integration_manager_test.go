package manifest

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	projectnetwork "github.com/s12ryt/s12ryt-vps-sh/internal/network"
	projectsystem "github.com/s12ryt/s12ryt-vps-sh/internal/system"
)

func TestIntegrationManagerAppliesAddressesRoutesFirewallThenSavesManifest(t *testing.T) {
	events := []string{}
	repository := &recordingManifestRepository{loadErr: os.ErrNotExist, events: &events}
	runner := &recordingSystemRunner{events: &events}
	manager, err := NewIntegrationManager(repository, runner)
	if err != nil {
		t.Fatalf("NewIntegrationManager() error = %v", err)
	}
	candidate := manifestFixture()
	if err := manager.Apply(context.Background(), candidate); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if repository.current == nil || repository.current.Addresses[0] != candidate.Addresses[0] {
		t.Fatalf("saved manifest = %#v", repository.current)
	}
	assertEventOrder(t, events,
		"load",
		"ip -6 addr add 2001:db8:100::10/64 dev eth0",
		"ip -6 route replace default via fe80::1 dev eth0 src 2001:db8:100::10 table 42000",
		"nft add table inet s12ryt-ipv6",
		"save",
	)
	if strings.Contains(strings.Join(events, "\n"), "addr del") {
		t.Fatalf("successful apply executed rollback: %#v", events)
	}
}

func TestIntegrationManagerRollsBackAttemptedGroupsWhenApplyOrSaveFails(t *testing.T) {
	applyFailure := errors.New("firewall apply failed")
	saveFailure := errors.New("manifest save failed")
	for name, arrange := range map[string]func(*recordingManifestRepository, *recordingSystemRunner){
		"apply failure": func(_ *recordingManifestRepository, runner *recordingSystemRunner) {
			runner.failContains = "nft add table inet s12ryt-ipv6"
			runner.failure = applyFailure
		},
		"save failure": func(repository *recordingManifestRepository, _ *recordingSystemRunner) {
			repository.saveErr = saveFailure
		},
	} {
		t.Run(name, func(t *testing.T) {
			events := []string{}
			repository := &recordingManifestRepository{loadErr: os.ErrNotExist, events: &events}
			runner := &recordingSystemRunner{events: &events}
			arrange(repository, runner)
			manager, err := NewIntegrationManager(repository, runner)
			if err != nil {
				t.Fatalf("NewIntegrationManager() error = %v", err)
			}
			err = manager.Apply(context.Background(), manifestFixture())
			target := applyFailure
			if name == "save failure" {
				target = saveFailure
			}
			if !errors.Is(err, target) {
				t.Fatalf("Apply() error = %v, want %v", err, target)
			}
			if repository.current != nil {
				t.Fatalf("failed apply persisted manifest: %#v", repository.current)
			}
			assertEventOrder(t, events,
				"nft delete table inet s12ryt-ipv6",
				"ip -6 rule del from 2001:db8:100::11/128 lookup 42001 priority 22001",
				"ip -6 addr del 2001:db8:100::10/64 dev eth0",
			)
		})
	}
}

func TestIntegrationManagerReplacesExistingManifestTransactionally(t *testing.T) {
	events := []string{}
	current := manifestFixture()
	repository := &recordingManifestRepository{current: &current, events: &events}
	runner := &recordingSystemRunner{events: &events}
	manager, err := NewIntegrationManager(repository, runner)
	if err != nil {
		t.Fatalf("NewIntegrationManager() error = %v", err)
	}
	candidate := replacementManifestFixture()
	if err := manager.Replace(context.Background(), candidate); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	if repository.current == nil || repository.current.Addresses[0] != candidate.Addresses[0] {
		t.Fatalf("saved replacement manifest = %#v", repository.current)
	}
	assertEventOrder(t, events,
		"load",
		"nft delete table inet s12ryt-ipv6",
		"ip -6 route flush table 42000",
		"ip -6 addr del 2001:db8:100::10/64 dev eth0",
		"ip -6 addr add 2001:db8:200::20/64 dev eth0",
		"ip -6 route replace default via fe80::2 dev eth0 src 2001:db8:200::20 table 42000",
		"ufw allow proto tcp from 192.0.2.0/24 to any port 35555 comment s12ryt-ipv6",
		"save",
	)
	if eventEquals(events, "delete") {
		t.Fatalf("replacement deleted protected manifest: %#v", events)
	}
}

func TestIntegrationManagerUpsertsNewAndExistingManifest(t *testing.T) {
	for name, testCase := range map[string]struct {
		current *Manifest
	}{
		"new integration":      {},
		"existing integration": {current: manifestPointer(manifestFixture())},
	} {
		t.Run(name, func(t *testing.T) {
			events := []string{}
			repository := &recordingManifestRepository{current: testCase.current, events: &events}
			runner := &recordingSystemRunner{events: &events}
			manager, err := NewIntegrationManager(repository, runner)
			if err != nil {
				t.Fatalf("NewIntegrationManager() error = %v", err)
			}
			candidate := replacementManifestFixture()
			if err := manager.Upsert(context.Background(), candidate); err != nil {
				t.Fatalf("Upsert() error = %v", err)
			}
			if repository.current == nil || repository.current.Addresses[0] != candidate.Addresses[0] {
				t.Fatalf("upserted manifest = %#v", repository.current)
			}
			if !eventContains(events, "ip -6 addr add 2001:db8:200::20/64 dev eth0") {
				t.Fatalf("upsert did not apply candidate: %#v", events)
			}
			if name == "existing integration" && !eventContains(events, "nft delete table inet s12ryt-ipv6") {
				t.Fatalf("upsert did not replace existing integration: %#v", events)
			}
		})
	}
}

func TestIntegrationManagerRestoresCurrentManifestWhenReplacementFails(t *testing.T) {
	cleanupFailure := errors.New("old firewall cleanup failed")
	applyFailure := errors.New("new route apply failed")
	saveFailure := errors.New("replacement save failed")

	for name, testCase := range map[string]struct {
		failContains string
		runnerError  error
		saveError    error
		want         error
	}{
		"old cleanup": {
			failContains: "nft delete table inet s12ryt-ipv6",
			runnerError:  cleanupFailure,
			want:         cleanupFailure,
		},
		"new apply": {
			failContains: "route replace default via fe80::2",
			runnerError:  applyFailure,
			want:         applyFailure,
		},
		"manifest save": {
			saveError: saveFailure,
			want:      saveFailure,
		},
	} {
		t.Run(name, func(t *testing.T) {
			events := []string{}
			current := manifestFixture()
			repository := &recordingManifestRepository{
				current: &current,
				events:  &events,
				saveErr: testCase.saveError,
			}
			runner := &recordingSystemRunner{
				events:       &events,
				failContains: testCase.failContains,
				failure:      testCase.runnerError,
			}
			manager, err := NewIntegrationManager(repository, runner)
			if err != nil {
				t.Fatalf("NewIntegrationManager() error = %v", err)
			}
			err = manager.Replace(context.Background(), replacementManifestFixture())
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Replace() error = %v, want errors.Is(%v)", err, testCase.want)
			}
			if repository.current == nil || repository.current.Addresses[0] != current.Addresses[0] {
				t.Fatalf("failed replacement changed protected manifest: %#v", repository.current)
			}
			if eventEquals(events, "delete") {
				t.Fatalf("failed replacement deleted protected manifest: %#v", events)
			}

			if name == "old cleanup" {
				if eventContains(events, "2001:db8:200::20") || eventEquals(events, "save") {
					t.Fatalf("old cleanup failure started replacement: %#v", events)
				}
				return
			}
			assertEventOrder(t, events,
				"ip -6 addr del 2001:db8:200::20/64 dev eth0",
				"ip -6 addr add 2001:db8:100::10/64 dev eth0",
				"ip -6 route replace default via fe80::1 dev eth0 src 2001:db8:100::10 table 42000",
				"nft add table inet s12ryt-ipv6",
			)
		})
	}
}

func TestIntegrationManagerCleanupDeletesManifestOnlyAfterAllResources(t *testing.T) {
	events := []string{}
	current := manifestFixture()
	repository := &recordingManifestRepository{current: &current, events: &events}
	runner := &recordingSystemRunner{events: &events}
	manager, err := NewIntegrationManager(repository, runner)
	if err != nil {
		t.Fatalf("NewIntegrationManager() error = %v", err)
	}
	if err := manager.Remove(context.Background()); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	assertEventOrder(t, events,
		"load",
		"nft delete table inet s12ryt-ipv6",
		"ip -6 route flush table 42000",
		"ip -6 addr del 2001:db8:100::10/64 dev eth0",
		"delete",
	)
	if repository.current != nil {
		t.Fatalf("manifest remains after cleanup: %#v", repository.current)
	}
}

func TestIntegrationManagerCleanupKeepsManifestWhenACommandFails(t *testing.T) {
	cleanupFailure := errors.New("route cleanup failed")
	events := []string{}
	current := manifestFixture()
	repository := &recordingManifestRepository{current: &current, events: &events}
	runner := &recordingSystemRunner{
		events:       &events,
		failContains: "ip -6 route flush table 42000",
		failure:      cleanupFailure,
	}
	manager, err := NewIntegrationManager(repository, runner)
	if err != nil {
		t.Fatalf("NewIntegrationManager() error = %v", err)
	}
	err = manager.Remove(context.Background())
	if !errors.Is(err, cleanupFailure) {
		t.Fatalf("Remove() error = %v, want cleanup failure", err)
	}
	if repository.current == nil {
		t.Fatal("manifest deleted after incomplete cleanup")
	}
	if !eventContains(events, "ip -6 addr del 2001:db8:100::10/64 dev eth0") {
		t.Fatalf("cleanup stopped before remaining project resources: %#v", events)
	}
	if eventEquals(events, "delete") {
		t.Fatalf("cleanup deleted manifest after command failure: %#v", events)
	}
}

func TestIntegrationManagerRestoresManifestWithoutRewritingProtectedState(t *testing.T) {
	events := []string{}
	current := manifestFixture()
	repository := &recordingManifestRepository{current: &current, events: &events}
	runner := &recordingSystemRunner{events: &events}
	manager, err := NewIntegrationManager(repository, runner)
	if err != nil {
		t.Fatalf("NewIntegrationManager() error = %v", err)
	}
	if err := manager.Restore(context.Background()); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	assertEventOrder(t, events,
		"load",
		"ip -6 addr add 2001:db8:100::10/64 dev eth0",
		"ip -6 route replace default via fe80::1 dev eth0 src 2001:db8:100::10 table 42000",
		"nft add table inet s12ryt-ipv6",
	)
	if eventEquals(events, "save") || eventEquals(events, "delete") {
		t.Fatalf("restore rewrote protected manifest: %#v", events)
	}
}

func TestIntegrationManagerRestoreIsNoOpWithoutManifest(t *testing.T) {
	events := []string{}
	repository := &recordingManifestRepository{loadErr: os.ErrNotExist, events: &events}
	runner := &recordingSystemRunner{events: &events}
	manager, err := NewIntegrationManager(repository, runner)
	if err != nil {
		t.Fatalf("NewIntegrationManager() error = %v", err)
	}
	if err := manager.Restore(context.Background()); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if len(events) != 1 || events[0] != "load" {
		t.Fatalf("restore without manifest events = %#v, want load only", events)
	}
}

func TestIntegrationManagerRestoreRollsBackAttemptedGroupsOnFailure(t *testing.T) {
	restoreFailure := errors.New("firewall restore failed")
	events := []string{}
	current := manifestFixture()
	repository := &recordingManifestRepository{current: &current, events: &events}
	runner := &recordingSystemRunner{
		events:       &events,
		failContains: "nft add table inet s12ryt-ipv6",
		failure:      restoreFailure,
	}
	manager, err := NewIntegrationManager(repository, runner)
	if err != nil {
		t.Fatalf("NewIntegrationManager() error = %v", err)
	}
	err = manager.Restore(context.Background())
	if !errors.Is(err, restoreFailure) {
		t.Fatalf("Restore() error = %v, want restore failure", err)
	}
	assertEventOrder(t, events,
		"nft delete table inet s12ryt-ipv6",
		"ip -6 rule del from 2001:db8:100::11/128 lookup 42001 priority 22001",
		"ip -6 addr del 2001:db8:100::10/64 dev eth0",
	)
	if eventEquals(events, "save") || eventEquals(events, "delete") {
		t.Fatalf("failed restore changed protected manifest: %#v", events)
	}
}

func TestIntegrationManagerRejectsUnsafeInputsBeforeSideEffects(t *testing.T) {
	if _, err := NewIntegrationManager(nil, &recordingSystemRunner{}); err == nil {
		t.Fatal("NewIntegrationManager(nil repository) succeeded")
	}
	if _, err := NewIntegrationManager(&recordingManifestRepository{}, nil); err == nil {
		t.Fatal("NewIntegrationManager(nil runner) succeeded")
	}
	events := []string{}
	repository := &recordingManifestRepository{loadErr: os.ErrNotExist, events: &events}
	manager, err := NewIntegrationManager(repository, &recordingSystemRunner{events: &events})
	if err != nil {
		t.Fatalf("NewIntegrationManager() error = %v", err)
	}
	invalid := manifestFixture()
	invalid.Interface = "eth0;reboot"
	if err := manager.Apply(context.Background(), invalid); err == nil {
		t.Fatal("Apply(invalid) error = nil")
	}
	if err := manager.Replace(context.Background(), invalid); err == nil {
		t.Fatal("Replace(invalid) error = nil")
	}
	if len(events) != 0 {
		t.Fatalf("invalid manifest caused side effects: %#v", events)
	}
	if err := manager.Replace(nil, manifestFixture()); err == nil {
		t.Fatal("Replace(nil context) error = nil")
	}
	if err := manager.Restore(nil); err == nil {
		t.Fatal("Restore(nil context) error = nil")
	}
}

type recordingManifestRepository struct {
	current   *Manifest
	loadErr   error
	saveErr   error
	deleteErr error
	events    *[]string
}

func (repository *recordingManifestRepository) Load() (Manifest, error) {
	repository.record("load")
	if repository.loadErr != nil {
		return Manifest{}, repository.loadErr
	}
	if repository.current == nil {
		return Manifest{}, os.ErrNotExist
	}
	return *repository.current, nil
}

func (repository *recordingManifestRepository) Save(value Manifest) error {
	repository.record("save")
	if repository.saveErr != nil {
		return repository.saveErr
	}
	repository.current = &value
	return nil
}

func (repository *recordingManifestRepository) Remove() error {
	repository.record("delete")
	if repository.deleteErr != nil {
		return repository.deleteErr
	}
	repository.current = nil
	return nil
}

func (repository *recordingManifestRepository) record(event string) {
	if repository.events != nil {
		*repository.events = append(*repository.events, event)
	}
}

type recordingSystemRunner struct {
	events       *[]string
	failContains string
	failure      error
}

func (runner *recordingSystemRunner) Run(_ context.Context, command projectsystem.Command) error {
	event := command.Name + " " + strings.Join(command.Args, " ")
	if runner.events != nil {
		*runner.events = append(*runner.events, event)
	}
	if runner.failContains != "" && strings.Contains(event, runner.failContains) {
		return runner.failure
	}
	return nil
}

func assertEventOrder(t *testing.T, events []string, expected ...string) {
	t.Helper()
	position := -1
	for _, value := range expected {
		found := -1
		for index := position + 1; index < len(events); index++ {
			if strings.Contains(events[index], value) {
				found = index
				break
			}
		}
		if found < 0 {
			t.Fatalf("event %q not found after position %d: %#v", value, position, events)
		}
		position = found
	}
}

func eventContains(events []string, expected string) bool {
	for _, event := range events {
		if strings.Contains(event, expected) {
			return true
		}
	}
	return false
}

func eventEquals(events []string, expected string) bool {
	for _, event := range events {
		if event == expected {
			return true
		}
	}
	return false
}

func replacementManifestFixture() Manifest {
	return Manifest{
		SchemaVersion: SchemaVersion,
		Interface:     "eth0",
		Prefix:        "2001:db8:200::/64",
		Gateway:       "fe80::2",
		Addresses:     []string{"2001:db8:200::20", "2001:db8:200::21"},
		Firewall: FirewallManifest{
			Backend:      projectnetwork.FirewallUFW,
			PanelPort:    35555,
			AllowedCIDRs: []string{"192.0.2.0/24", "2001:db8:ffff::/64"},
			NodePorts: []PortManifest{
				{Port: 25001, Protocol: "tcp"},
				{Port: 25002, Protocol: "udp"},
			},
		},
	}
}

func manifestPointer(value Manifest) *Manifest {
	return &value
}
