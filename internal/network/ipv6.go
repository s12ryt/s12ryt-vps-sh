package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"regexp"
)

const maxIPv6PoolSize = 256

var interfaceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

type InterfaceAddress struct {
	Interface string
	Prefix    netip.Prefix
}

type AddressPlan struct {
	Interface string
	Prefix    netip.Prefix
	Gateway   netip.Addr
	Addresses []netip.Addr
}

type Command struct {
	Name string
	Args []string
}

func ParseGlobalIPv6Addresses(payload []byte) ([]InterfaceAddress, error) {
	var links []struct {
		Interface string `json:"ifname"`
		Addresses []struct {
			Family    string `json:"family"`
			Local     string `json:"local"`
			PrefixLen int    `json:"prefixlen"`
			Scope     string `json:"scope"`
			Tentative bool   `json:"tentative"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal(payload, &links); err != nil {
		return nil, fmt.Errorf("parse IPv6 addresses: %w", err)
	}

	addresses := make([]InterfaceAddress, 0)
	for _, link := range links {
		if !validInterfaceName(link.Interface) {
			return nil, fmt.Errorf("invalid interface name %q", link.Interface)
		}
		for _, item := range link.Addresses {
			if item.Family != "inet6" || item.Scope != "global" || item.Tentative {
				continue
			}
			address, err := netip.ParseAddr(item.Local)
			if err != nil || !address.Is6() || !address.IsGlobalUnicast() {
				continue
			}
			if item.PrefixLen < 1 || item.PrefixLen > 127 {
				continue
			}
			addresses = append(addresses, InterfaceAddress{
				Interface: link.Interface,
				Prefix:    netip.PrefixFrom(address, item.PrefixLen),
			})
		}
	}
	return addresses, nil
}

func FormatIPv6Full(address netip.Addr) string {
	if !address.Is6() {
		return ""
	}
	bytes := address.As16()
	return fmt.Sprintf(
		"%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x",
		bytes[0], bytes[1], bytes[2], bytes[3], bytes[4], bytes[5], bytes[6], bytes[7],
		bytes[8], bytes[9], bytes[10], bytes[11], bytes[12], bytes[13], bytes[14], bytes[15],
	)
}

func ParseUniqueIPv6Gateway(payload []byte, interfaceName string) (netip.Addr, error) {
	if !validInterfaceName(interfaceName) {
		return netip.Addr{}, errors.New("invalid interface name")
	}
	var routes []struct {
		Destination string `json:"dst"`
		Gateway     string `json:"gateway"`
		Interface   string `json:"dev"`
	}
	if err := json.Unmarshal(payload, &routes); err != nil {
		return netip.Addr{}, fmt.Errorf("parse IPv6 routes: %w", err)
	}

	gateways := make([]netip.Addr, 0, 1)
	for _, route := range routes {
		if route.Destination != "default" || route.Interface != interfaceName {
			continue
		}
		gateway, err := netip.ParseAddr(route.Gateway)
		if err != nil || !gateway.Is6() {
			continue
		}
		gateways = append(gateways, gateway)
	}
	if len(gateways) != 1 {
		return netip.Addr{}, fmt.Errorf("expected one IPv6 gateway for %s, found %d", interfaceName, len(gateways))
	}
	return gateways[0], nil
}

func GenerateIPv6Pool(prefix netip.Prefix, count int, existing []netip.Addr, entropy io.Reader) ([]netip.Addr, error) {
	if !prefix.IsValid() || !prefix.Addr().Is6() || !prefix.Addr().IsGlobalUnicast() || prefix.Bits() >= 128 {
		return nil, errors.New("a global IPv6 prefix with host space is required")
	}
	if count < 1 || count > maxIPv6PoolSize {
		return nil, fmt.Errorf("IPv6 pool size must be between 1 and %d", maxIPv6PoolSize)
	}
	if entropy == nil {
		return nil, errors.New("entropy reader is required")
	}

	network := prefix.Masked()
	used := make(map[netip.Addr]struct{}, len(existing)+count)
	for _, address := range existing {
		if address.IsValid() {
			used[address.Unmap()] = struct{}{}
		}
	}

	pool := make([]netip.Addr, 0, count)
	for len(pool) < count {
		var random [16]byte
		if _, err := io.ReadFull(entropy, random[:]); err != nil {
			return nil, fmt.Errorf("read IPv6 entropy: %w", err)
		}
		candidate := addressInPrefix(network, random)
		if !candidate.IsGlobalUnicast() || !network.Contains(candidate) {
			continue
		}
		if _, duplicate := used[candidate]; duplicate {
			continue
		}
		used[candidate] = struct{}{}
		pool = append(pool, candidate)
	}
	return pool, nil
}

func BuildIPv6AddressCommands(plan AddressPlan) ([]Command, error) {
	if err := validateAddressPlan(plan); err != nil {
		return nil, err
	}
	commands := make([]Command, 0, len(plan.Addresses)+1)
	for _, address := range plan.Addresses {
		commands = append(commands, Command{
			Name: "ip",
			Args: []string{"-6", "addr", "add", fmt.Sprintf("%s/%d", address, plan.Prefix.Bits()), "dev", plan.Interface},
		})
	}
	commands = append(commands, Command{
		Name: "ip",
		Args: []string{"-6", "addr", "show", "dev", plan.Interface, "tentative"},
	})
	return commands, nil
}

func BuildIPv6RemovalCommands(plan AddressPlan) []Command {
	if !validInterfaceName(plan.Interface) || !plan.Prefix.IsValid() {
		return nil
	}
	commands := make([]Command, 0, len(plan.Addresses))
	for _, address := range plan.Addresses {
		if !address.Is6() || !plan.Prefix.Contains(address) {
			continue
		}
		commands = append(commands, Command{
			Name: "ip",
			Args: []string{"-6", "addr", "del", fmt.Sprintf("%s/%d", address, plan.Prefix.Bits()), "dev", plan.Interface},
		})
	}
	return commands
}

func addressInPrefix(prefix netip.Prefix, random [16]byte) netip.Addr {
	network := prefix.Addr().As16()
	wholeBytes := prefix.Bits() / 8
	remainingBits := prefix.Bits() % 8
	copy(random[:wholeBytes], network[:wholeBytes])
	if remainingBits > 0 {
		mask := byte(0xff << (8 - remainingBits))
		random[wholeBytes] = network[wholeBytes]&mask | random[wholeBytes]&^mask
	}
	return netip.AddrFrom16(random)
}

func validateAddressPlan(plan AddressPlan) error {
	if !validInterfaceName(plan.Interface) {
		return errors.New("invalid interface name")
	}
	if !plan.Prefix.IsValid() || !plan.Prefix.Addr().Is6() || plan.Prefix.Bits() >= 128 {
		return errors.New("invalid IPv6 prefix")
	}
	if !plan.Gateway.IsValid() || !plan.Gateway.Is6() {
		return errors.New("invalid IPv6 gateway")
	}
	if len(plan.Addresses) == 0 {
		return errors.New("at least one project IPv6 address is required")
	}
	seen := make(map[netip.Addr]struct{}, len(plan.Addresses))
	for _, address := range plan.Addresses {
		if !address.Is6() || !plan.Prefix.Contains(address) {
			return fmt.Errorf("address %s is outside the configured prefix", address)
		}
		if _, duplicate := seen[address]; duplicate {
			return fmt.Errorf("duplicate IPv6 address %s", address)
		}
		seen[address] = struct{}{}
	}
	return nil
}

func validInterfaceName(name string) bool {
	return name != "" && interfaceNamePattern.MatchString(name)
}
