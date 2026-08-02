package network

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
)

const (
	FirewallUFW       = "ufw"
	FirewallFirewalld = "firewalld"
	FirewallNFTables  = "nftables"
	FirewallMarker    = "s12ryt-ipv6"
)

type FirewallStatus struct {
	UFWActive       bool
	FirewalldActive bool
	NFTablesActive  bool
}

type PortRule struct {
	Port     int
	Protocol string
}

type FirewallInput struct {
	Backend      string
	PanelPort    int
	AllowedCIDRs []string
	NodePorts    []PortRule
}

type FirewallRule struct {
	CIDR     string
	Port     int
	Protocol string
	Purpose  string
}

type FirewallPlan struct {
	Marker string
	Rules  []FirewallRule
	Apply  []Command
	Remove []Command
}

func DetectFirewallBackend(status FirewallStatus) (string, error) {
	active := make([]string, 0, 3)
	if status.UFWActive {
		active = append(active, FirewallUFW)
	}
	if status.FirewalldActive {
		active = append(active, FirewallFirewalld)
	}
	if status.NFTablesActive {
		active = append(active, FirewallNFTables)
	}
	if len(active) != 1 {
		return "", fmt.Errorf("expected one active supported firewall, found %d", len(active))
	}
	return active[0], nil
}

func BuildFirewallPlan(input FirewallInput) (FirewallPlan, error) {
	if !supportedFirewallBackend(input.Backend) {
		return FirewallPlan{}, fmt.Errorf("unsupported firewall backend %q", input.Backend)
	}
	if input.PanelPort < 1 || input.PanelPort > 65535 {
		return FirewallPlan{}, errors.New("panel port must be between 1 and 65535")
	}
	if len(input.AllowedCIDRs) == 0 {
		return FirewallPlan{}, errors.New("at least one panel CIDR is required")
	}

	rules := make([]FirewallRule, 0, len(input.AllowedCIDRs)+len(input.NodePorts))
	seenCIDRs := make(map[string]struct{}, len(input.AllowedCIDRs))
	for _, cidr := range input.AllowedCIDRs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return FirewallPlan{}, fmt.Errorf("invalid CIDR %q", cidr)
		}
		canonical := prefix.Masked().String()
		if _, duplicate := seenCIDRs[canonical]; duplicate {
			return FirewallPlan{}, fmt.Errorf("duplicate panel CIDR %q", canonical)
		}
		seenCIDRs[canonical] = struct{}{}
		rules = append(rules, FirewallRule{CIDR: canonical, Port: input.PanelPort, Protocol: "tcp", Purpose: "panel"})
	}

	seenPorts := make(map[string]struct{}, len(input.NodePorts))
	for _, port := range input.NodePorts {
		if port.Port < 20000 || port.Port > 49999 {
			return FirewallPlan{}, fmt.Errorf("node port %d is outside 20000-49999", port.Port)
		}
		if port.Protocol != "tcp" && port.Protocol != "udp" {
			return FirewallPlan{}, fmt.Errorf("unsupported node protocol %q", port.Protocol)
		}
		key := fmt.Sprintf("%s/%d", port.Protocol, port.Port)
		if _, duplicate := seenPorts[key]; duplicate {
			return FirewallPlan{}, fmt.Errorf("duplicate node rule %s", key)
		}
		seenPorts[key] = struct{}{}
		rules = append(rules, FirewallRule{Port: port.Port, Protocol: port.Protocol, Purpose: "node"})
	}

	apply, remove := buildFirewallCommands(input.Backend, rules)
	return FirewallPlan{Marker: FirewallMarker, Rules: rules, Apply: apply, Remove: remove}, nil
}

func supportedFirewallBackend(backend string) bool {
	return backend == FirewallUFW || backend == FirewallFirewalld || backend == FirewallNFTables
}

func buildFirewallCommands(backend string, rules []FirewallRule) ([]Command, []Command) {
	switch backend {
	case FirewallUFW:
		return buildUFWCommands(rules)
	case FirewallFirewalld:
		return buildFirewalldCommands(rules)
	default:
		return buildNFTablesCommands(rules)
	}
}

func buildUFWCommands(rules []FirewallRule) ([]Command, []Command) {
	apply := make([]Command, 0, len(rules))
	remove := make([]Command, 0, len(rules))
	for _, rule := range rules {
		args := ufwRuleArgs(rule)
		apply = append(apply, Command{Name: "ufw", Args: append([]string{"allow"}, args...)})
		remove = append(remove, Command{Name: "ufw", Args: append([]string{"delete", "allow"}, args...)})
	}
	return apply, remove
}

func ufwRuleArgs(rule FirewallRule) []string {
	args := []string{"proto", rule.Protocol}
	if rule.CIDR != "" {
		args = append(args, "from", rule.CIDR)
	}
	args = append(args, "to", "any", "port", strconv.Itoa(rule.Port), "comment", FirewallMarker)
	return args
}

func buildFirewalldCommands(rules []FirewallRule) ([]Command, []Command) {
	zone := FirewallMarker
	apply := []Command{{Name: "firewall-cmd", Args: []string{"--permanent", "--new-zone=" + zone}}}
	remove := []Command{{Name: "firewall-cmd", Args: []string{"--permanent", "--delete-zone=" + zone}}}
	for _, rule := range rules {
		richRule := firewalldRichRule(rule)
		apply = append(apply, Command{Name: "firewall-cmd", Args: []string{"--permanent", "--zone=" + zone, "--add-rich-rule=" + richRule}})
		remove = append(remove, Command{Name: "firewall-cmd", Args: []string{"--permanent", "--zone=" + zone, "--remove-rich-rule=" + richRule}})
	}
	return apply, remove
}

func firewalldRichRule(rule FirewallRule) string {
	family := "ipv4"
	if rule.CIDR != "" {
		if prefix, err := netip.ParsePrefix(rule.CIDR); err == nil && prefix.Addr().Is6() {
			family = "ipv6"
		}
	}
	result := fmt.Sprintf(`rule family="%s"`, family)
	if rule.CIDR != "" {
		result += fmt.Sprintf(` source address="%s"`, rule.CIDR)
	}
	return result + fmt.Sprintf(` port port="%d" protocol="%s" accept`, rule.Port, rule.Protocol)
}

func buildNFTablesCommands(rules []FirewallRule) ([]Command, []Command) {
	table := FirewallMarker
	apply := []Command{
		{Name: "nft", Args: []string{"add", "table", "inet", table}},
		{Name: "nft", Args: []string{"add", "chain", "inet", table, "input", "{", "type", "filter", "hook", "input", "priority", "0", ";", "policy", "accept", ";", "}"}},
	}
	for _, rule := range rules {
		args := []string{"add", "rule", "inet", table, "input"}
		if rule.CIDR != "" {
			family := "ip"
			if prefix, err := netip.ParsePrefix(rule.CIDR); err == nil && prefix.Addr().Is6() {
				family = "ip6"
			}
			args = append(args, family, "saddr", rule.CIDR)
		}
		args = append(args, rule.Protocol, "dport", strconv.Itoa(rule.Port), "accept", "comment", FirewallMarker)
		apply = append(apply, Command{Name: "nft", Args: args})
	}
	remove := []Command{{Name: "nft", Args: []string{"delete", "table", "inet", table}}}
	return apply, remove
}
