package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	projectnetwork "github.com/s12ryt/s12ryt-vps-sh/internal/network"
)

func TestStoreProtectsManifestAndBuildsProjectOnlyCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "integration.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	current := manifestFixture()
	if err := store.Save(current); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(loaded, current) {
		t.Fatalf("Load() = %#v, want %#v", loaded, current)
	}
	assertManifestMode(t, filepath.Dir(path), 0o700)
	assertManifestMode(t, path, 0o600)

	cleanup, err := loaded.CleanupPlan()
	if err != nil {
		t.Fatalf("CleanupPlan() error = %v", err)
	}
	if len(cleanup.Firewall) != 1 || !reflect.DeepEqual(cleanup.Firewall[0], projectnetwork.Command{
		Name: "nft",
		Args: []string{"delete", "table", "inet", projectnetwork.FirewallMarker},
	}) {
		t.Fatalf("firewall cleanup = %#v", cleanup.Firewall)
	}
	if len(cleanup.Routes) != 4 {
		t.Fatalf("route cleanup count = %d, want 4: %#v", len(cleanup.Routes), cleanup.Routes)
	}
	if len(cleanup.Addresses) != 2 {
		t.Fatalf("address cleanup count = %d, want 2: %#v", len(cleanup.Addresses), cleanup.Addresses)
	}
	commands := formatManifestCommands(append(append(cleanup.Firewall, cleanup.Routes...), cleanup.Addresses...))
	for _, required := range []string{
		"ip -6 rule del from 2001:db8:100::11/128 lookup 42001 priority 22001",
		"ip -6 route flush table 42000",
		"ip -6 addr del 2001:db8:100::10/64 dev eth0",
		"ip -6 addr del 2001:db8:100::11/64 dev eth0",
	} {
		if !strings.Contains(commands, required) {
			t.Fatalf("cleanup commands missing %q:\n%s", required, commands)
		}
	}
	for _, forbidden := range []string{"2001:db8:100::99", " table main", "route del default"} {
		if strings.Contains(commands, forbidden) {
			t.Fatalf("cleanup commands contain unmanaged operation %q:\n%s", forbidden, commands)
		}
	}
}

func TestStoreKeepsBackupAndRejectsInvalidReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "integration.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	original := manifestFixture()
	if err := store.Save(original); err != nil {
		t.Fatalf("Save(original) error = %v", err)
	}

	updated := manifestFixture()
	updated.Addresses = append(updated.Addresses, "2001:db8:100::12")
	if err := store.Save(updated); err != nil {
		t.Fatalf("Save(updated) error = %v", err)
	}
	backupStore, err := NewStore(path + ".bak")
	if err != nil {
		t.Fatalf("NewStore(backup) error = %v", err)
	}
	backup, err := backupStore.Load()
	if err != nil {
		t.Fatalf("Load(backup) error = %v", err)
	}
	if !reflect.DeepEqual(backup, original) {
		t.Fatalf("backup = %#v, want original %#v", backup, original)
	}

	invalid := updated
	invalid.SchemaVersion = 99
	if err := store.Save(invalid); err == nil {
		t.Fatal("Save(invalid) error = nil, want rejection")
	}
	current, err := store.Load()
	if err != nil {
		t.Fatalf("Load(current) error = %v", err)
	}
	if !reflect.DeepEqual(current, updated) {
		t.Fatalf("current changed after rejected save: %#v", current)
	}
}

func TestManifestRejectsUnsafeIntegrationState(t *testing.T) {
	for name, mutate := range map[string]func(*Manifest){
		"schema":               func(value *Manifest) { value.SchemaVersion = 2 },
		"interface":            func(value *Manifest) { value.Interface = "eth0;reboot" },
		"IPv4 prefix":          func(value *Manifest) { value.Prefix = "198.51.100.0/24" },
		"IPv4 gateway":         func(value *Manifest) { value.Gateway = "198.51.100.1" },
		"no addresses":         func(value *Manifest) { value.Addresses = nil },
		"outside address":      func(value *Manifest) { value.Addresses[0] = "2001:db8:200::10" },
		"duplicate address":    func(value *Manifest) { value.Addresses[1] = value.Addresses[0] },
		"unsupported firewall": func(value *Manifest) { value.Firewall.Backend = "iptables" },
		"privileged node port": func(value *Manifest) { value.Firewall.NodePorts[0].Port = 443 },
	} {
		t.Run(name, func(t *testing.T) {
			value := manifestFixture()
			mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want rejection")
			}
		})
	}
}

func TestStoreRejectsUnknownUnprotectedAndLinkedManifest(t *testing.T) {
	for name, arrange := range map[string]func(*testing.T, string){
		"unknown field": func(t *testing.T, path string) {
			t.Helper()
			payload := `{"schema_version":1,"interface":"eth0","prefix":"2001:db8:100::/64","gateway":"fe80::1","addresses":["2001:db8:100::10"],"firewall":{"backend":"nftables","panel_port":34456,"allowed_cidrs":["0.0.0.0/0"],"node_ports":[]},"unknown":true}`
			if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
		},
		"group readable": func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
		},
		"symbolic link": func(t *testing.T, path string) {
			t.Helper()
			target := path + ".target"
			if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
				t.Fatalf("WriteFile(target) error = %v", err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatalf("Symlink() error = %v", err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "integration.json")
			arrange(t, path)
			store, err := NewStore(path)
			if err != nil {
				t.Fatalf("NewStore() error = %v", err)
			}
			if _, err := store.Load(); err == nil {
				t.Fatal("Load() error = nil, want protected strict manifest rejection")
			}
		})
	}
}

func manifestFixture() Manifest {
	return Manifest{
		SchemaVersion: SchemaVersion,
		Interface:     "eth0",
		Prefix:        "2001:db8:100::/64",
		Gateway:       "fe80::1",
		Addresses:     []string{"2001:db8:100::10", "2001:db8:100::11"},
		Firewall: FirewallManifest{
			Backend:      projectnetwork.FirewallNFTables,
			PanelPort:    34456,
			AllowedCIDRs: []string{"0.0.0.0/0", "::/0"},
			NodePorts: []PortManifest{
				{Port: 24001, Protocol: "tcp"},
				{Port: 24002, Protocol: "udp"},
			},
		},
	}
}

func assertManifestMode(t *testing.T, path string, expected os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if info.Mode().Perm() != expected {
		t.Fatalf("mode(%s) = %04o, want %04o", path, info.Mode().Perm(), expected)
	}
}

func formatManifestCommands(commands []projectnetwork.Command) string {
	lines := make([]string, 0, len(commands))
	for _, command := range commands {
		lines = append(lines, command.Name+" "+strings.Join(command.Args, " "))
	}
	return strings.Join(lines, "\n")
}
