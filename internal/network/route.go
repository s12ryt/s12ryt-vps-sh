package network

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
)

const policyRouteTableBase = 42000
const policyRoutePriorityBase = 22000

type PolicyRouteInput struct {
	Interface string
	Gateway   string
	Addresses []string
}

type PolicyRoutePlan struct {
	Apply  []Command
	Remove []Command
}

func BuildPolicyRoutePlan(input PolicyRouteInput) (PolicyRoutePlan, error) {
	if !validInterfaceName(input.Interface) {
		return PolicyRoutePlan{}, fmt.Errorf("invalid policy route interface %q", input.Interface)
	}
	gateway, err := netip.ParseAddr(input.Gateway)
	if err != nil || !gateway.Is6() || gateway.IsUnspecified() || gateway.IsMulticast() || gateway.IsLoopback() {
		return PolicyRoutePlan{}, errors.New("policy route gateway must be a usable IPv6 address")
	}
	if len(input.Addresses) == 0 || len(input.Addresses) > 256 {
		return PolicyRoutePlan{}, errors.New("policy route requires between 1 and 256 IPv6 addresses")
	}

	addresses := make([]netip.Addr, 0, len(input.Addresses))
	seen := make(map[netip.Addr]struct{}, len(input.Addresses))
	for _, value := range input.Addresses {
		address, parseErr := netip.ParseAddr(value)
		if parseErr != nil || !address.Is6() || !address.IsGlobalUnicast() {
			return PolicyRoutePlan{}, fmt.Errorf("policy route source %q must be global IPv6", value)
		}
		address = address.Unmap()
		if _, duplicate := seen[address]; duplicate {
			return PolicyRoutePlan{}, fmt.Errorf("duplicate policy route source %q", address)
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}

	plan := PolicyRoutePlan{
		Apply:  make([]Command, 0, len(addresses)*2),
		Remove: make([]Command, 0, len(addresses)*2),
	}
	for index, address := range addresses {
		table := strconv.Itoa(policyRouteTableBase + index)
		priority := strconv.Itoa(policyRoutePriorityBase + index)
		source := address.String()
		plan.Apply = append(plan.Apply,
			Command{Name: "ip", Args: []string{"-6", "route", "replace", "default", "via", gateway.String(), "dev", input.Interface, "src", source, "table", table}},
			Command{Name: "ip", Args: []string{"-6", "rule", "add", "from", source + "/128", "lookup", table, "priority", priority}},
		)
	}
	for index := len(addresses) - 1; index >= 0; index-- {
		table := strconv.Itoa(policyRouteTableBase + index)
		priority := strconv.Itoa(policyRoutePriorityBase + index)
		source := addresses[index].String()
		plan.Remove = append(plan.Remove,
			Command{Name: "ip", Args: []string{"-6", "rule", "del", "from", source + "/128", "lookup", table, "priority", priority}},
			Command{Name: "ip", Args: []string{"-6", "route", "flush", "table", table}},
		)
	}
	return plan, nil
}
