package network

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildPolicyRoutePlanUsesDedicatedTablesForProjectIPv6(t *testing.T) {
	plan, err := BuildPolicyRoutePlan(PolicyRouteInput{
		Interface: "eth0",
		Gateway:   "fe80::1",
		Addresses: []string{"2001:db8:100::10", "2001:db8:100::20"},
	})
	if err != nil {
		t.Fatalf("BuildPolicyRoutePlan() error = %v", err)
	}
	if len(plan.Apply) != 4 || len(plan.Remove) != 4 {
		t.Fatalf("commands apply/remove = %d/%d, want 4/4", len(plan.Apply), len(plan.Remove))
	}

	wantApply := [][]string{
		{"-6", "route", "replace", "default", "via", "fe80::1", "dev", "eth0", "src", "2001:db8:100::10", "table", "42000"},
		{"-6", "rule", "add", "from", "2001:db8:100::10/128", "lookup", "42000", "priority", "22000"},
		{"-6", "route", "replace", "default", "via", "fe80::1", "dev", "eth0", "src", "2001:db8:100::20", "table", "42001"},
		{"-6", "rule", "add", "from", "2001:db8:100::20/128", "lookup", "42001", "priority", "22001"},
	}
	for index, want := range wantApply {
		assertCommand(t, plan.Apply[index], "ip", want)
	}

	wantRemove := [][]string{
		{"-6", "rule", "del", "from", "2001:db8:100::20/128", "lookup", "42001", "priority", "22001"},
		{"-6", "route", "flush", "table", "42001"},
		{"-6", "rule", "del", "from", "2001:db8:100::10/128", "lookup", "42000", "priority", "22000"},
		{"-6", "route", "flush", "table", "42000"},
	}
	for index, want := range wantRemove {
		assertCommand(t, plan.Remove[index], "ip", want)
	}
}

func TestPolicyRoutePlanNeverChangesMainDefaultRoute(t *testing.T) {
	plan, err := BuildPolicyRoutePlan(PolicyRouteInput{
		Interface: "ens3",
		Gateway:   "2001:db8:1::1",
		Addresses: []string{"2001:db8:1::50"},
	})
	if err != nil {
		t.Fatalf("BuildPolicyRoutePlan() error = %v", err)
	}
	for _, command := range append(append([]Command(nil), plan.Apply...), plan.Remove...) {
		joined := strings.Join(command.Args, " ")
		if strings.Contains(joined, "table main") ||
			(strings.Contains(joined, "route replace default") && !strings.Contains(joined, " table ")) {
			t.Fatalf("command changes the main default route: %s %s", command.Name, joined)
		}
	}
}

func TestBuildPolicyRoutePlanCanonicalizesAddressesAndRejectsDuplicates(t *testing.T) {
	plan, err := BuildPolicyRoutePlan(PolicyRouteInput{
		Interface: "eth0",
		Gateway:   "FE80:0:0:0:0:0:0:1",
		Addresses: []string{"2001:0db8:0100::0010"},
	})
	if err != nil {
		t.Fatalf("BuildPolicyRoutePlan() error = %v", err)
	}
	joined := strings.Join(plan.Apply[0].Args, " ")
	if !strings.Contains(joined, "via fe80::1") || !strings.Contains(joined, "src 2001:db8:100::10") {
		t.Fatalf("command does not use canonical addresses: %s", joined)
	}

	_, err = BuildPolicyRoutePlan(PolicyRouteInput{
		Interface: "eth0",
		Gateway:   "fe80::1",
		Addresses: []string{"2001:db8::1", "2001:0db8:0:0:0:0:0:1"},
	})
	if err == nil {
		t.Fatal("BuildPolicyRoutePlan() accepted duplicate canonical addresses")
	}
}

func TestBuildPolicyRoutePlanRejectsUnsafeInputs(t *testing.T) {
	tests := []PolicyRouteInput{
		{Interface: "", Gateway: "fe80::1", Addresses: []string{"2001:db8::1"}},
		{Interface: "eth0;reboot", Gateway: "fe80::1", Addresses: []string{"2001:db8::1"}},
		{Interface: "eth0", Gateway: "", Addresses: []string{"2001:db8::1"}},
		{Interface: "eth0", Gateway: "192.0.2.1", Addresses: []string{"2001:db8::1"}},
		{Interface: "eth0", Gateway: "::", Addresses: []string{"2001:db8::1"}},
		{Interface: "eth0", Gateway: "fe80::1"},
		{Interface: "eth0", Gateway: "fe80::1", Addresses: []string{"fe80::2"}},
		{Interface: "eth0", Gateway: "fe80::1", Addresses: []string{"192.0.2.2"}},
	}
	tooMany := PolicyRouteInput{Interface: "eth0", Gateway: "fe80::1"}
	for index := range 257 {
		tooMany.Addresses = append(tooMany.Addresses, "2001:db8::"+decimalString(index+1))
	}
	tests = append(tests, tooMany)

	for index, input := range tests {
		if _, err := BuildPolicyRoutePlan(input); err == nil {
			t.Fatalf("BuildPolicyRoutePlan() accepted unsafe case %d", index)
		}
	}
}

func decimalString(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(digits[value%10]) + result
		value /= 10
	}
	return result
}

func assertCommand(t *testing.T, command Command, name string, args []string) {
	t.Helper()
	if command.Name != name || !reflect.DeepEqual(command.Args, args) {
		t.Fatalf("command = %q %q, want %q %q", command.Name, command.Args, name, args)
	}
}
