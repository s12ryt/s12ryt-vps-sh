package endpoint

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/manifest"
	projectsystem "github.com/s12ryt/s12ryt-vps-sh/internal/system"
)

func TestServiceRuntimeRestartsSupportedInitSystems(t *testing.T) {
	tests := map[string]projectsystem.Command{
		"systemd": {Name: "systemctl", Args: []string{"restart", "s12ryt-ipv6.service"}},
		"openrc":  {Name: "rc-service", Args: []string{"s12ryt-ipv6", "restart"}},
	}
	for initSystem, expected := range tests {
		t.Run(initSystem, func(t *testing.T) {
			runner := &endpointCommandRunner{}
			runtime, err := NewServiceRuntime(ServiceRuntimeOptions{
				InitSystem: initSystem,
				Runner:     runner,
				HTTPClient: &endpointHTTPClient{response: healthResponse(http.StatusOK, `{"status":"ok"}`)},
			})
			if err != nil {
				t.Fatalf("NewServiceRuntime() error = %v", err)
			}
			if err := runtime.Restart(context.Background(), domain.DefaultConfig().Panel); err != nil {
				t.Fatalf("Restart() error = %v", err)
			}
			if !reflect.DeepEqual(runner.commands, []projectsystem.Command{expected}) {
				t.Fatalf("commands = %#v, want %#v", runner.commands, []projectsystem.Command{expected})
			}
		})
	}
}

func TestServiceRuntimeChecksCandidateLoopbackHealth(t *testing.T) {
	client := &endpointHTTPClient{response: healthResponse(http.StatusOK, `{"status":"ok"}`)}
	runtime, err := NewServiceRuntime(ServiceRuntimeOptions{
		InitSystem: "systemd",
		Runner:     &endpointCommandRunner{},
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("NewServiceRuntime() error = %v", err)
	}
	panel := domain.PanelConfig{
		Port:         35555,
		Path:         "/newpanel1234",
		ListenIPv6:   "2001:db8:100::20",
		AllowedCIDRs: []string{"0.0.0.0/0", "::/0"},
	}
	if err := runtime.Healthy(context.Background(), panel); err != nil {
		t.Fatalf("Healthy() error = %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(client.requests))
	}
	request := client.requests[0]
	if request.Method != http.MethodGet || request.URL.String() != "http://127.0.0.1:35555/newpanel1234/healthz" {
		t.Fatalf("health request = %s %s", request.Method, request.URL)
	}
}

func TestServiceRuntimeRejectsUnhealthyResponses(t *testing.T) {
	probeErr := errors.New("probe failed")
	tests := map[string]*endpointHTTPClient{
		"request": {err: probeErr},
		"status":  {response: healthResponse(http.StatusServiceUnavailable, `{"status":"ok"}`)},
		"body":    {response: healthResponse(http.StatusOK, `{"status":"starting"}`)},
		"large":   {response: healthResponse(http.StatusOK, strings.Repeat("x", 4097))},
	}
	for name, client := range tests {
		t.Run(name, func(t *testing.T) {
			runtime, err := NewServiceRuntime(ServiceRuntimeOptions{
				InitSystem: "systemd",
				Runner:     &endpointCommandRunner{},
				HTTPClient: client,
			})
			if err != nil {
				t.Fatalf("NewServiceRuntime() error = %v", err)
			}
			err = runtime.Healthy(context.Background(), domain.DefaultConfig().Panel)
			if err == nil {
				t.Fatal("Healthy() accepted an unhealthy response")
			}
			if name == "request" && !errors.Is(err, probeErr) {
				t.Fatalf("Healthy() error = %v, want probe error", err)
			}
		})
	}
}

func TestManifestNetworkReplacesOnlyPanelFirewallSettings(t *testing.T) {
	current := endpointManifest()
	repository := &endpointManifestRepository{current: current}
	replacer := &endpointManifestReplacer{repository: repository}
	network, err := NewManifestNetwork(repository, replacer)
	if err != nil {
		t.Fatalf("NewManifestNetwork() error = %v", err)
	}

	currentPanel := domain.PanelConfig{Port: 34456, Path: "/configureme1", AllowedCIDRs: []string{"0.0.0.0/0", "::/0"}}
	candidatePanel := domain.PanelConfig{Port: 35555, Path: "/newpanel1234", ListenIPv6: "2001:db8:100::20", AllowedCIDRs: []string{"198.51.100.0/24", "2001:db8::/32"}}
	if err := network.ReplacePanel(context.Background(), currentPanel, candidatePanel); err != nil {
		t.Fatalf("ReplacePanel() error = %v", err)
	}
	if replacer.calls != 1 {
		t.Fatalf("Replace() calls = %d, want 1", replacer.calls)
	}
	want := current
	want.Firewall.PanelPort = 35555
	want.Firewall.AllowedCIDRs = []string{"198.51.100.0/24", "2001:db8::/32"}
	if !reflect.DeepEqual(replacer.candidate, want) {
		t.Fatalf("replacement manifest = %#v, want %#v", replacer.candidate, want)
	}
}

func TestManifestNetworkSkipsMissingManifestAndRejectsStaleEndpoint(t *testing.T) {
	replacer := &endpointManifestReplacer{}
	network, err := NewManifestNetwork(&endpointManifestRepository{loadErr: errors.New("wrapped manifest error")}, replacer)
	if err != nil {
		t.Fatalf("NewManifestNetwork() error = %v", err)
	}
	if err := network.ReplacePanel(context.Background(), domain.DefaultConfig().Panel, domain.DefaultConfig().Panel); err == nil {
		t.Fatal("ReplacePanel() ignored a non-os.ErrNotExist load error")
	}

	missingNetwork, err := NewManifestNetwork(&endpointManifestRepository{missing: true}, replacer)
	if err != nil {
		t.Fatalf("NewManifestNetwork(missing) error = %v", err)
	}
	if err := missingNetwork.ReplacePanel(context.Background(), domain.DefaultConfig().Panel, domain.DefaultConfig().Panel); err != nil {
		t.Fatalf("ReplacePanel(missing) error = %v", err)
	}
	if replacer.calls != 0 {
		t.Fatalf("Replace() calls = %d, want 0", replacer.calls)
	}

	current := endpointManifest()
	staleNetwork, err := NewManifestNetwork(&endpointManifestRepository{current: current}, replacer)
	if err != nil {
		t.Fatalf("NewManifestNetwork(stale) error = %v", err)
	}
	stale := domain.DefaultConfig().Panel
	stale.Port = 30000
	if err := staleNetwork.ReplacePanel(context.Background(), stale, domain.DefaultConfig().Panel); err == nil {
		t.Fatal("ReplacePanel() accepted stale current firewall settings")
	}
	if replacer.calls != 0 {
		t.Fatalf("Replace() calls = %d after stale endpoint, want 0", replacer.calls)
	}
}

func TestEndpointAdaptersRejectMissingDependencies(t *testing.T) {
	runner := &endpointCommandRunner{}
	client := &endpointHTTPClient{response: healthResponse(http.StatusOK, `{"status":"ok"}`)}
	for name, options := range map[string]ServiceRuntimeOptions{
		"init":         {Runner: runner, HTTPClient: client},
		"runner":       {InitSystem: "systemd", HTTPClient: client},
		"client":       {InitSystem: "systemd", Runner: runner},
		"unknown init": {InitSystem: "unknown", Runner: runner, HTTPClient: client},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewServiceRuntime(options); err == nil {
				t.Fatal("NewServiceRuntime() accepted invalid options")
			}
		})
	}
	if _, err := NewManifestNetwork(nil, &endpointManifestReplacer{}); err == nil {
		t.Fatal("NewManifestNetwork() accepted nil repository")
	}
	if _, err := NewManifestNetwork(&endpointManifestRepository{}, nil); err == nil {
		t.Fatal("NewManifestNetwork() accepted nil replacer")
	}
}

type endpointCommandRunner struct {
	commands []projectsystem.Command
	err      error
}

func (runner *endpointCommandRunner) Run(_ context.Context, command projectsystem.Command) error {
	runner.commands = append(runner.commands, command)
	return runner.err
}

type endpointHTTPClient struct {
	requests []*http.Request
	response *http.Response
	err      error
}

func (client *endpointHTTPClient) Do(request *http.Request) (*http.Response, error) {
	client.requests = append(client.requests, request)
	return client.response, client.err
}

func healthResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     make(http.Header),
	}
}

type endpointManifestRepository struct {
	current manifest.Manifest
	loadErr error
	missing bool
}

func (repository *endpointManifestRepository) Load() (manifest.Manifest, error) {
	if repository.missing {
		return manifest.Manifest{}, os.ErrNotExist
	}
	if repository.loadErr != nil {
		return manifest.Manifest{}, repository.loadErr
	}
	return repository.current, nil
}

type endpointManifestReplacer struct {
	repository *endpointManifestRepository
	candidate  manifest.Manifest
	calls      int
}

func (replacer *endpointManifestReplacer) Replace(_ context.Context, candidate manifest.Manifest) error {
	replacer.calls++
	replacer.candidate = candidate
	if replacer.repository != nil {
		replacer.repository.current = candidate
	}
	return nil
}

func endpointManifest() manifest.Manifest {
	return manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Interface:     "eth0",
		Prefix:        "2001:db8:100::/64",
		Gateway:       "fe80::1",
		Addresses:     []string{"2001:db8:100::10", "2001:db8:100::11"},
		Firewall: manifest.FirewallManifest{
			Backend:      "nftables",
			PanelPort:    34456,
			AllowedCIDRs: []string{"0.0.0.0/0", "::/0"},
			NodePorts: []manifest.PortManifest{
				{Port: 25000, Protocol: "tcp"},
				{Port: 25000, Protocol: "udp"},
			},
		},
	}
}
