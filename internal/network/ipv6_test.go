package network

import (
	"bytes"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestParseGlobalIPv6AddressesFiltersAndFormatsFullAddresses(t *testing.T) {
	payload := []byte(`[
		{"ifname":"eth0","addr_info":[
			{"family":"inet6","local":"2001:db8:12::10","prefixlen":64,"scope":"global"},
			{"family":"inet6","local":"fe80::1","prefixlen":64,"scope":"link"}
		]},
		{"ifname":"eth1","addr_info":[
			{"family":"inet","local":"192.0.2.10","prefixlen":24,"scope":"global"},
			{"family":"inet6","local":"2001:db8:34::20","prefixlen":64,"scope":"global","tentative":true}
		]}
	]`)

	addresses, err := ParseGlobalIPv6Addresses(payload)
	if err != nil {
		t.Fatalf("ParseGlobalIPv6Addresses() error = %v", err)
	}
	want := []InterfaceAddress{{Interface: "eth0", Prefix: netip.MustParsePrefix("2001:db8:12::10/64")}}
	if !reflect.DeepEqual(addresses, want) {
		t.Fatalf("addresses = %#v, want %#v", addresses, want)
	}
	if formatted := FormatIPv6Full(addresses[0].Prefix.Addr()); formatted != "2001:0db8:0012:0000:0000:0000:0000:0010" {
		t.Fatalf("FormatIPv6Full() = %q", formatted)
	}
}

func TestParseUniqueIPv6GatewayRequiresOneRouteForInterface(t *testing.T) {
	one := []byte(`[{
		"dst":"default","gateway":"2001:db8:12::1","dev":"eth0","metric":100
	}]`)
	gateway, err := ParseUniqueIPv6Gateway(one, "eth0")
	if err != nil || gateway != netip.MustParseAddr("2001:db8:12::1") {
		t.Fatalf("ParseUniqueIPv6Gateway(one) = %v, %v", gateway, err)
	}

	for name, payload := range map[string][]byte{
		"missing":   []byte(`[]`),
		"ambiguous": []byte(`[{"dst":"default","gateway":"2001:db8:12::1","dev":"eth0"},{"dst":"default","gateway":"2001:db8:12::2","dev":"eth0"}]`),
		"wrong dev": []byte(`[{"dst":"default","gateway":"2001:db8:12::1","dev":"eth1"}]`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseUniqueIPv6Gateway(payload, "eth0"); err == nil {
				t.Fatal("ParseUniqueIPv6Gateway() accepted a non-unique route")
			}
		})
	}
}

func TestGenerateIPv6PoolIsDeterministicUniqueAndExcludesExisting(t *testing.T) {
	prefix := netip.MustParsePrefix("2001:db8:abcd::/64")
	existing := []netip.Addr{netip.MustParseAddr("2001:db8:abcd::1")}
	entropy := append(bytes.Repeat([]byte{0}, 15), 1)
	entropy = append(entropy, append(bytes.Repeat([]byte{0}, 15), 2)...)
	entropy = append(entropy, append(bytes.Repeat([]byte{0}, 15), 3)...)

	pool, err := GenerateIPv6Pool(prefix, 2, existing, bytes.NewReader(entropy))
	if err != nil {
		t.Fatalf("GenerateIPv6Pool() error = %v", err)
	}
	want := []netip.Addr{
		netip.MustParseAddr("2001:db8:abcd::2"),
		netip.MustParseAddr("2001:db8:abcd::3"),
	}
	if !reflect.DeepEqual(pool, want) {
		t.Fatalf("pool = %v, want %v", pool, want)
	}
}

func TestGenerateIPv6PoolRejectsUnsafeInputs(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		count  int
	}{
		{name: "IPv4 prefix", prefix: "192.0.2.0/24", count: 1},
		{name: "non-global prefix", prefix: "fe80::/64", count: 1},
		{name: "zero count", prefix: "2001:db8:1::/64", count: 0},
		{name: "too many", prefix: "2001:db8:1::/64", count: 257},
		{name: "no host space", prefix: "2001:db8:1::1/128", count: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prefix := netip.MustParsePrefix(test.prefix)
			if _, err := GenerateIPv6Pool(prefix, test.count, nil, bytes.NewReader(bytes.Repeat([]byte{1}, 64))); err == nil {
				t.Fatal("GenerateIPv6Pool() accepted unsafe input")
			}
		})
	}
}

func TestBuildIPv6AddressCommandsOnlyTouchesProjectAddresses(t *testing.T) {
	plan := AddressPlan{
		Interface: "eth0",
		Prefix:    netip.MustParsePrefix("2001:db8:abcd::/64"),
		Gateway:   netip.MustParseAddr("2001:db8:abcd::1"),
		Addresses: []netip.Addr{
			netip.MustParseAddr("2001:db8:abcd::10"),
			netip.MustParseAddr("2001:db8:abcd::11"),
		},
	}
	commands, err := BuildIPv6AddressCommands(plan)
	if err != nil {
		t.Fatalf("BuildIPv6AddressCommands() error = %v", err)
	}
	want := []Command{
		{Name: "ip", Args: []string{"-6", "addr", "add", "2001:db8:abcd::10/64", "dev", "eth0"}},
		{Name: "ip", Args: []string{"-6", "addr", "add", "2001:db8:abcd::11/64", "dev", "eth0"}},
		{Name: "ip", Args: []string{"-6", "addr", "show", "dev", "eth0", "tentative"}},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	for _, command := range commands {
		joined := command.Name + " " + strings.Join(command.Args, " ")
		if strings.Contains(joined, "route replace default") || strings.Contains(joined, "route del default") || strings.Contains(joined, "nodad") {
			t.Fatalf("unsafe command planned: %s", joined)
		}
	}

	remove := BuildIPv6RemovalCommands(plan)
	if len(remove) != 2 || strings.Join(remove[0].Args, " ") != "-6 addr del 2001:db8:abcd::10/64 dev eth0" {
		t.Fatalf("removal commands = %#v", remove)
	}
}
