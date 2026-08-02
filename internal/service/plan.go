package service

import (
	"errors"
	"io/fs"
)

const InitSystemd = "systemd"
const InitOpenRC = "openrc"
const projectRoot = "/opt/s12ryt-ipv6"

type Options struct {
	InitSystem  string
	ProjectRoot string
}

type File struct {
	Mode    fs.FileMode
	Content string
}

type Command struct {
	Name string
	Args []string
}

type Plan struct {
	Files       map[string]File
	Install     []Command
	Remove      []Command
	RemoveFiles []string
}

func BuildPlan(options Options) (Plan, error) {
	if options.ProjectRoot == "" {
		options.ProjectRoot = projectRoot
	}
	if options.ProjectRoot != projectRoot {
		return Plan{}, errors.New("service project root must be /opt/s12ryt-ipv6")
	}
	switch options.InitSystem {
	case InitSystemd:
		return buildSystemdPlan(), nil
	case InitOpenRC:
		return buildOpenRCPlan(), nil
	default:
		return Plan{}, errors.New("unsupported init system")
	}
}

func buildSystemdPlan() Plan {
	const unitPath = "/etc/systemd/system/s12ryt-ipv6.service"
	return Plan{
		Files: map[string]File{
			unitPath: {
				Mode: 0o644,
				Content: `[Unit]
Description=s12ryt IPv6 outbound panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
ExecStart=/opt/s12ryt-ipv6/bin/s12ryt-ipv6
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/opt/s12ryt-ipv6
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
`,
			},
		},
		Install: []Command{
			{Name: "systemctl", Args: []string{"daemon-reload"}},
			{Name: "systemctl", Args: []string{"enable", "--now", "s12ryt-ipv6.service"}},
		},
		Remove: []Command{
			{Name: "systemctl", Args: []string{"disable", "--now", "s12ryt-ipv6.service"}},
			{Name: "systemctl", Args: []string{"daemon-reload"}},
		},
		RemoveFiles: []string{unitPath},
	}
}

func buildOpenRCPlan() Plan {
	const servicePath = "/etc/init.d/s12ryt-ipv6"
	const logrotatePath = "/etc/logrotate.d/s12ryt-ipv6"
	return Plan{
		Files: map[string]File{
			servicePath: {
				Mode: 0o755,
				Content: `#!/sbin/openrc-run
name="s12ryt IPv6 outbound panel"
description="s12ryt IPv6 outbound panel"
command=/opt/s12ryt-ipv6/bin/s12ryt-ipv6
command_background=true
pidfile=/run/s12ryt-ipv6.pid
output_log=/var/log/s12ryt-ipv6/panel.log
error_log=/var/log/s12ryt-ipv6/panel.log

depend() {
  need net
  after firewall
}
`,
			},
			logrotatePath: {
				Mode: 0o644,
				Content: `/var/log/s12ryt-ipv6/*.log {
  daily
  rotate 7
  size 100M
  missingok
  notifempty
  copytruncate
}
`,
			},
		},
		Install: []Command{
			{Name: "rc-update", Args: []string{"add", "s12ryt-ipv6", "default"}},
			{Name: "rc-service", Args: []string{"s12ryt-ipv6", "start"}},
		},
		Remove: []Command{
			{Name: "rc-service", Args: []string{"s12ryt-ipv6", "stop"}},
			{Name: "rc-update", Args: []string{"del", "s12ryt-ipv6", "default"}},
		},
		RemoveFiles: []string{servicePath, logrotatePath},
	}
}
