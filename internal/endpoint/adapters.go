package endpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/manifest"
	projectsystem "github.com/s12ryt/s12ryt-vps-sh/internal/system"
)

const endpointHealthResponseLimit = 4096

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type ServiceRuntimeOptions struct {
	InitSystem string
	Runner     projectsystem.Runner
	HTTPClient HTTPClient
}

type ServiceRuntime struct {
	initSystem string
	runner     projectsystem.Runner
	httpClient HTTPClient
}

func NewServiceRuntime(options ServiceRuntimeOptions) (*ServiceRuntime, error) {
	if options.InitSystem != "systemd" && options.InitSystem != "openrc" {
		return nil, errors.New("endpoint runtime requires systemd or OpenRC")
	}
	if options.Runner == nil {
		return nil, errors.New("endpoint runtime command runner is required")
	}
	if options.HTTPClient == nil {
		return nil, errors.New("endpoint runtime HTTP client is required")
	}
	return &ServiceRuntime{
		initSystem: options.InitSystem,
		runner:     options.Runner,
		httpClient: options.HTTPClient,
	}, nil
}

func (runtime *ServiceRuntime) Restart(ctx context.Context, panel domain.PanelConfig) error {
	if ctx == nil {
		return errors.New("endpoint restart context is required")
	}
	if err := validatePanelConfig(panel); err != nil {
		return err
	}
	command := projectsystem.Command{Name: "systemctl", Args: []string{"restart", "s12ryt-ipv6.service"}}
	if runtime.initSystem == "openrc" {
		command = projectsystem.Command{Name: "rc-service", Args: []string{"s12ryt-ipv6", "restart"}}
	}
	if err := runtime.runner.Run(ctx, command); err != nil {
		return fmt.Errorf("restart endpoint service: %w", err)
	}
	return nil
}

func (runtime *ServiceRuntime) Healthy(ctx context.Context, panel domain.PanelConfig) error {
	if ctx == nil {
		return errors.New("endpoint health context is required")
	}
	if err := validatePanelConfig(panel); err != nil {
		return err
	}
	healthURL := "http://127.0.0.1:" + strconv.Itoa(panel.Port) + panel.Path + "/healthz"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return fmt.Errorf("build endpoint health request: %w", err)
	}
	response, err := runtime.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request endpoint health: %w", err)
	}
	if response == nil || response.Body == nil {
		return errors.New("endpoint health response is empty")
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, endpointHealthResponseLimit+1))
	if err != nil {
		return fmt.Errorf("read endpoint health response: %w", err)
	}
	if len(payload) > endpointHealthResponseLimit {
		return errors.New("endpoint health response exceeds the size limit")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("endpoint health returned HTTP %d", response.StatusCode)
	}
	if !bytes.Equal(bytes.TrimSpace(payload), []byte(`{"status":"ok"}`)) {
		return errors.New("endpoint health response is invalid")
	}
	return nil
}

type ManifestRepository interface {
	Load() (manifest.Manifest, error)
}

type ManifestReplacer interface {
	Replace(context.Context, manifest.Manifest) error
}

type ManifestNetwork struct {
	repository ManifestRepository
	replacer   ManifestReplacer
}

func NewManifestNetwork(repository ManifestRepository, replacer ManifestReplacer) (*ManifestNetwork, error) {
	if repository == nil {
		return nil, errors.New("endpoint manifest repository is required")
	}
	if replacer == nil {
		return nil, errors.New("endpoint manifest replacer is required")
	}
	return &ManifestNetwork{repository: repository, replacer: replacer}, nil
}

func (network *ManifestNetwork) ReplacePanel(ctx context.Context, current, candidate domain.PanelConfig) error {
	if ctx == nil {
		return errors.New("endpoint network replacement context is required")
	}
	if err := validatePanelConfig(current); err != nil {
		return fmt.Errorf("validate current panel endpoint: %w", err)
	}
	if err := validatePanelConfig(candidate); err != nil {
		return fmt.Errorf("validate replacement panel endpoint: %w", err)
	}
	currentManifest, err := network.repository.Load()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load endpoint integration manifest: %w", err)
	}
	if currentManifest.Firewall.PanelPort != current.Port ||
		!slices.Equal(currentManifest.Firewall.AllowedCIDRs, current.AllowedCIDRs) {
		return errors.New("current endpoint does not match the integration manifest")
	}

	replacement := cloneManifest(currentManifest)
	replacement.Firewall.PanelPort = candidate.Port
	replacement.Firewall.AllowedCIDRs = append([]string(nil), candidate.AllowedCIDRs...)
	if err := replacement.Validate(); err != nil {
		return fmt.Errorf("validate replacement integration manifest: %w", err)
	}
	if err := network.replacer.Replace(ctx, replacement); err != nil {
		return fmt.Errorf("replace endpoint integration manifest: %w", err)
	}
	return nil
}

func validatePanelConfig(panel domain.PanelConfig) error {
	config := domain.DefaultConfig()
	config.Panel = panel
	if err := config.Validate(); err != nil {
		return fmt.Errorf("validate panel endpoint: %w", err)
	}
	return nil
}

func cloneManifest(value manifest.Manifest) manifest.Manifest {
	result := value
	result.Addresses = append([]string(nil), value.Addresses...)
	result.Firewall.AllowedCIDRs = append([]string(nil), value.Firewall.AllowedCIDRs...)
	result.Firewall.NodePorts = append([]manifest.PortManifest(nil), value.Firewall.NodePorts...)
	return result
}
