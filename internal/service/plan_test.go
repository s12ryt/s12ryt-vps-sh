package service

import (
	"strings"
	"testing"
)

func TestBuildSystemdPlanUsesProjectUnitAndHardening(t *testing.T) {
	plan, err := BuildPlan(Options{InitSystem: InitSystemd})
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	unit, exists := plan.Files["/etc/systemd/system/s12ryt-ipv6.service"]
	if !exists {
		t.Fatal("systemd unit was not planned")
	}
	if unit.Mode != 0o644 {
		t.Fatalf("unit mode = %04o, want 0644", unit.Mode)
	}
	for _, fragment := range []string{
		"User=root",
		"ExecStart=/opt/s12ryt-ipv6/bin/s12ryt-ipv6",
		"Restart=on-failure",
		"NoNewPrivileges=true",
		"ProtectSystem=strict",
		"ReadWritePaths=/opt/s12ryt-ipv6",
		"CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE",
	} {
		if !strings.Contains(unit.Content, fragment) {
			t.Fatalf("unit missing %q:\n%s", fragment, unit.Content)
		}
	}
	assertCommands(t, plan.Install, [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "--now", "s12ryt-ipv6.service"},
	})
	assertCommands(t, plan.Remove, [][]string{
		{"systemctl", "disable", "--now", "s12ryt-ipv6.service"},
		{"systemctl", "daemon-reload"},
	})
}

func TestBuildOpenRCPlanUsesForegroundSupervisorAndLogFiles(t *testing.T) {
	plan, err := BuildPlan(Options{InitSystem: InitOpenRC})
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	serviceFile, exists := plan.Files["/etc/init.d/s12ryt-ipv6"]
	if !exists {
		t.Fatal("OpenRC service was not planned")
	}
	if serviceFile.Mode != 0o755 {
		t.Fatalf("OpenRC service mode = %04o, want 0755", serviceFile.Mode)
	}
	for _, fragment := range []string{
		"#!/sbin/openrc-run",
		"command=/opt/s12ryt-ipv6/bin/s12ryt-ipv6",
		"command_background=true",
		"pidfile=/run/s12ryt-ipv6.pid",
		"output_log=/var/log/s12ryt-ipv6/panel.log",
		"error_log=/var/log/s12ryt-ipv6/panel.log",
	} {
		if !strings.Contains(serviceFile.Content, fragment) {
			t.Fatalf("OpenRC service missing %q:\n%s", fragment, serviceFile.Content)
		}
	}
	logrotate, exists := plan.Files["/etc/logrotate.d/s12ryt-ipv6"]
	if !exists || !strings.Contains(logrotate.Content, "rotate 7") || !strings.Contains(logrotate.Content, "size 100M") {
		t.Fatalf("OpenRC logrotate contract missing: %#v", logrotate)
	}
	assertCommands(t, plan.Install, [][]string{
		{"rc-update", "add", "s12ryt-ipv6", "default"},
		{"rc-service", "s12ryt-ipv6", "start"},
	})
	assertCommands(t, plan.Remove, [][]string{
		{"rc-service", "s12ryt-ipv6", "stop"},
		{"rc-update", "del", "s12ryt-ipv6", "default"},
	})
}

func TestBuildPlanRejectsUnsupportedInitAndCustomPaths(t *testing.T) {
	for name, options := range map[string]Options{
		"unknown init": {InitSystem: "runit"},
		"custom root":  {InitSystem: InitSystemd, ProjectRoot: "/tmp/project"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildPlan(options); err == nil {
				t.Fatal("BuildPlan accepted unsupported service options")
			}
		})
	}
}

func TestRemovalOnlyReferencesProjectOwnedServiceFiles(t *testing.T) {
	for _, initSystem := range []string{InitSystemd, InitOpenRC} {
		plan, err := BuildPlan(Options{InitSystem: initSystem})
		if err != nil {
			t.Fatalf("BuildPlan(%s): %v", initSystem, err)
		}
		for _, path := range plan.RemoveFiles {
			if !strings.Contains(path, "s12ryt-ipv6") {
				t.Fatalf("removal path is not project-owned: %q", path)
			}
		}
	}
}

func assertCommands(t *testing.T, actual []Command, expected [][]string) {
	t.Helper()
	if len(actual) != len(expected) {
		t.Fatalf("command count = %d, want %d: %#v", len(actual), len(expected), actual)
	}
	for index, command := range actual {
		want := expected[index]
		if command.Name != want[0] || strings.Join(command.Args, "\x00") != strings.Join(want[1:], "\x00") {
			t.Fatalf("command %d = %#v, want %#v", index, command, want)
		}
	}
}
