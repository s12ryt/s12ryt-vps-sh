package networksetup

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"

	projectnetwork "github.com/s12ryt/s12ryt-vps-sh/internal/network"
)

func TestIPDiscoveryUsesStructuredAddressAndRouteQueries(t *testing.T) {
	runner := &recordingOutputRunner{responses: [][]byte{
		[]byte(`[
			{"ifname":"eth0","addr_info":[
				{"family":"inet6","local":"2001:db8:100::10","prefixlen":64,"scope":"global","tentative":false},
				{"family":"inet6","local":"fe80::1","prefixlen":64,"scope":"link","tentative":false}
			]}
		]`),
		[]byte(`[{
			"dst":"default","gateway":"fe80::1","dev":"eth0"
		}]`),
	}}
	discovery, err := NewIPDiscovery(runner)
	if err != nil {
		t.Fatalf("NewIPDiscovery() error = %v", err)
	}

	addresses, err := discovery.GlobalIPv6Addresses(context.Background())
	if err != nil {
		t.Fatalf("GlobalIPv6Addresses() error = %v", err)
	}
	wantAddresses := []projectnetwork.InterfaceAddress{
		{Interface: "eth0", Prefix: netip.MustParsePrefix("2001:db8:100::10/64")},
	}
	if !reflect.DeepEqual(addresses, wantAddresses) {
		t.Fatalf("GlobalIPv6Addresses() = %#v, want %#v", addresses, wantAddresses)
	}

	gateway, err := discovery.UniqueIPv6Gateway(context.Background(), "eth0")
	if err != nil {
		t.Fatalf("UniqueIPv6Gateway() error = %v", err)
	}
	if gateway != netip.MustParseAddr("fe80::1") {
		t.Fatalf("UniqueIPv6Gateway() = %s", gateway)
	}
	wantCalls := []outputCall{
		{Name: "ip", Args: []string{"-j", "-6", "addr", "show"}},
		{Name: "ip", Args: []string{"-j", "-6", "route", "show", "default"}},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestIPDiscoveryPreservesRunnerAndParserErrors(t *testing.T) {
	runnerErr := errors.New("runner failed")
	for name, testCase := range map[string]struct {
		runner *recordingOutputRunner
		action func(*IPDiscovery) error
		want   error
	}{
		"address runner": {
			runner: &recordingOutputRunner{err: runnerErr},
			action: func(discovery *IPDiscovery) error {
				_, err := discovery.GlobalIPv6Addresses(context.Background())
				return err
			},
			want: runnerErr,
		},
		"route runner": {
			runner: &recordingOutputRunner{err: runnerErr},
			action: func(discovery *IPDiscovery) error {
				_, err := discovery.UniqueIPv6Gateway(context.Background(), "eth0")
				return err
			},
			want: runnerErr,
		},
		"address parser": {
			runner: &recordingOutputRunner{responses: [][]byte{[]byte(`not-json`)}},
			action: func(discovery *IPDiscovery) error {
				_, err := discovery.GlobalIPv6Addresses(context.Background())
				return err
			},
		},
		"route parser": {
			runner: &recordingOutputRunner{responses: [][]byte{[]byte(`[]`)}},
			action: func(discovery *IPDiscovery) error {
				_, err := discovery.UniqueIPv6Gateway(context.Background(), "eth0")
				return err
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			discovery, err := NewIPDiscovery(testCase.runner)
			if err != nil {
				t.Fatalf("NewIPDiscovery() error = %v", err)
			}
			err = testCase.action(discovery)
			if err == nil {
				t.Fatal("discovery error = nil, want failure")
			}
			if testCase.want != nil && !errors.Is(err, testCase.want) {
				t.Fatalf("discovery error = %v, want errors.Is(%v)", err, testCase.want)
			}
		})
	}
}

func TestNewIPDiscoveryRejectsMissingRunner(t *testing.T) {
	if _, err := NewIPDiscovery(nil); err == nil {
		t.Fatal("NewIPDiscovery(nil) error = nil, want rejection")
	}
}

type outputCall struct {
	Name string
	Args []string
}

type recordingOutputRunner struct {
	calls     []outputCall
	responses [][]byte
	err       error
}

func (runner *recordingOutputRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	runner.calls = append(runner.calls, outputCall{Name: name, Args: append([]string(nil), args...)})
	if runner.err != nil {
		return nil, runner.err
	}
	if len(runner.responses) == 0 {
		return nil, nil
	}
	response := append([]byte(nil), runner.responses[0]...)
	runner.responses = runner.responses[1:]
	return response, nil
}
