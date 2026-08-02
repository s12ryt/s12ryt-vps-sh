package network

import (
	"strings"
	"testing"
)

func TestDetectFirewallBackendRequiresOneActiveSupportedBackend(t *testing.T) {
	tests := []struct {
		name    string
		status  FirewallStatus
		want    string
		wantErr bool
	}{
		{name: "ufw", status: FirewallStatus{UFWActive: true}, want: FirewallUFW},
		{name: "firewalld", status: FirewallStatus{FirewalldActive: true}, want: FirewallFirewalld},
		{name: "nftables", status: FirewallStatus{NFTablesActive: true}, want: FirewallNFTables},
		{name: "none", status: FirewallStatus{}, wantErr: true},
		{name: "ambiguous", status: FirewallStatus{UFWActive: true, FirewalldActive: true}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := DetectFirewallBackend(test.status)
			if test.wantErr {
				if err == nil {
					t.Fatal("DetectFirewallBackend accepted unsafe status")
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("DetectFirewallBackend = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestBuildFirewallPlanCreatesMinimalProjectOwnedRules(t *testing.T) {
	input := FirewallInput{
		PanelPort:   34456,
		AllowedCIDRs: []string{"198.51.100.0/24", "2001:db8:1::/64"},
		NodePorts: []PortRule{
			{Port: 24001, Protocol: "tcp"},
			{Port: 24002, Protocol: "udp"},
		},
	}
	for _, backend := range []string{FirewallUFW, FirewallFirewalld, FirewallNFTables} {
		t.Run(backend, func(t *testing.T) {
			input.Backend = backend
			plan, err := BuildFirewallPlan(input)
			if err != nil {
				t.Fatalf("BuildFirewallPlan returned error: %v", err)
			}
			if plan.Marker != FirewallMarker || len(plan.Rules) != 4 {
				t.Fatalf("unexpected plan identity/rules: %#v", plan)
			}
			if plan.Rules[0] != (FirewallRule{CIDR: "198.51.100.0/24", Port: 34456, Protocol: "tcp", Purpose: "panel"}) {
				t.Fatalf("first panel rule = %#v", plan.Rules[0])
			}
			if plan.Rules[1].CIDR != "2001:db8:1::/64" || plan.Rules[1].Purpose != "panel" {
				t.Fatalf("second panel rule = %#v", plan.Rules[1])
			}
			for _, command := range append(append([]Command{}, plan.Apply...), plan.Remove...) {
				joined := command.Name + " " + strings.Join(command.Args, " ")
				if !strings.Contains(joined, FirewallMarker) {
					t.Fatalf("command is not project-marked: %s", joined)
				}
			}
		})
	}
}

func TestBuildFirewallPlanRejectsUnsafeOrDuplicateRules(t *testing.T) {
	base := FirewallInput{
		Backend:      FirewallUFW,
		PanelPort:    34456,
		AllowedCIDRs: []string{"203.0.113.0/24"},
		NodePorts:    []PortRule{{Port: 24001, Protocol: "tcp"}},
	}
	tests := map[string]FirewallInput{
		"unknown backend":      withFirewallBackend(base, "iptables"),
		"invalid CIDR":         withAllowedCIDRs(base, []string{"all"}),
		"invalid panel port":   withPanelPort(base, 0),
		"privileged node port": withNodePorts(base, []PortRule{{Port: 443, Protocol: "tcp"}}),
		"invalid protocol":     withNodePorts(base, []PortRule{{Port: 24001, Protocol: "sctp"}}),
		"duplicate node rule":  withNodePorts(base, []PortRule{{Port: 24001, Protocol: "tcp"}, {Port: 24001, Protocol: "tcp"}}),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildFirewallPlan(input); err == nil {
				t.Fatal("BuildFirewallPlan accepted unsafe input")
			}
		})
	}
}

func withFirewallBackend(input FirewallInput, backend string) FirewallInput {
	input.Backend = backend
	return input
}

func withAllowedCIDRs(input FirewallInput, cidrs []string) FirewallInput {
	input.AllowedCIDRs = cidrs
	return input
}

func withPanelPort(input FirewallInput, port int) FirewallInput {
	input.PanelPort = port
	return input
}

func withNodePorts(input FirewallInput, ports []PortRule) FirewallInput {
	input.NodePorts = ports
	return input
}
