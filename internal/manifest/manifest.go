package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"

	projectnetwork "github.com/s12ryt/s12ryt-vps-sh/internal/network"
)

const SchemaVersion = 1

type Manifest struct {
	SchemaVersion int              `json:"schema_version"`
	Interface     string           `json:"interface"`
	Prefix        string           `json:"prefix"`
	Gateway       string           `json:"gateway"`
	Addresses     []string         `json:"addresses"`
	Firewall      FirewallManifest `json:"firewall"`
}

type FirewallManifest struct {
	Backend      string         `json:"backend"`
	PanelPort    int            `json:"panel_port"`
	AllowedCIDRs []string       `json:"allowed_cidrs"`
	NodePorts    []PortManifest `json:"node_ports"`
}

type PortManifest struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

type CleanupPlan struct {
	Firewall  []projectnetwork.Command
	Routes    []projectnetwork.Command
	Addresses []projectnetwork.Command
}

type Store struct {
	path string
}

func NewStore(path string) (*Store, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("manifest path must be absolute")
	}
	return &Store{path: path}, nil
}

func (manifest Manifest) Validate() error {
	if manifest.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported manifest schema version %d", manifest.SchemaVersion)
	}
	prefix, gateway, addresses, err := manifest.networkValues()
	if err != nil {
		return err
	}
	addressPlan := projectnetwork.AddressPlan{
		Interface: manifest.Interface,
		Prefix:    prefix,
		Gateway:   gateway,
		Addresses: addresses,
	}
	if _, err := projectnetwork.BuildIPv6AddressCommands(addressPlan); err != nil {
		return fmt.Errorf("validate project IPv6 addresses: %w", err)
	}
	if _, err := projectnetwork.BuildPolicyRoutePlan(projectnetwork.PolicyRouteInput{
		Interface: manifest.Interface,
		Gateway:   gateway.String(),
		Addresses: append([]string(nil), manifest.Addresses...),
	}); err != nil {
		return fmt.Errorf("validate project policy routes: %w", err)
	}
	if _, err := projectnetwork.BuildFirewallPlan(manifest.firewallInput()); err != nil {
		return fmt.Errorf("validate project firewall: %w", err)
	}
	return nil
}

func (manifest Manifest) CleanupPlan() (CleanupPlan, error) {
	if err := manifest.Validate(); err != nil {
		return CleanupPlan{}, err
	}
	prefix, gateway, addresses, err := manifest.networkValues()
	if err != nil {
		return CleanupPlan{}, err
	}
	firewall, err := projectnetwork.BuildFirewallPlan(manifest.firewallInput())
	if err != nil {
		return CleanupPlan{}, fmt.Errorf("build firewall cleanup: %w", err)
	}
	routes, err := projectnetwork.BuildPolicyRoutePlan(projectnetwork.PolicyRouteInput{
		Interface: manifest.Interface,
		Gateway:   gateway.String(),
		Addresses: append([]string(nil), manifest.Addresses...),
	})
	if err != nil {
		return CleanupPlan{}, fmt.Errorf("build policy route cleanup: %w", err)
	}
	addressPlan := projectnetwork.AddressPlan{
		Interface: manifest.Interface,
		Prefix:    prefix,
		Gateway:   gateway,
		Addresses: addresses,
	}
	return CleanupPlan{
		Firewall:  cloneCommands(firewall.Remove),
		Routes:    cloneCommands(routes.Remove),
		Addresses: cloneCommands(projectnetwork.BuildIPv6RemovalCommands(addressPlan)),
	}, nil
}

func (store *Store) Load() (Manifest, error) {
	payload, err := readProtectedManifest(store.path)
	if err != nil {
		return Manifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode integration manifest: %w", err)
	}
	if err := ensureManifestEOF(decoder); err != nil {
		return Manifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("validate integration manifest: %w", err)
	}
	return manifest, nil
}

func (store *Store) Save(manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("validate integration manifest: %w", err)
	}
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode integration manifest: %w", err)
	}
	payload = append(payload, '\n')
	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("protect manifest directory: %w", err)
	}

	temporaryPath, err := stageManifestFile(directory, filepath.Base(store.path), payload)
	if err != nil {
		return err
	}
	defer os.Remove(temporaryPath)

	current, err := readProtectedManifest(store.path)
	if err == nil {
		if err := replaceManifestFile(store.path+".bak", current); err != nil {
			return fmt.Errorf("backup integration manifest: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read current integration manifest: %w", err)
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		return fmt.Errorf("replace integration manifest: %w", err)
	}
	if err := syncManifestDirectory(directory); err != nil {
		return err
	}
	return nil
}

func (manifest Manifest) networkValues() (netip.Prefix, netip.Addr, []netip.Addr, error) {
	prefix, err := netip.ParsePrefix(manifest.Prefix)
	if err != nil || !prefix.Addr().Is6() || !prefix.Addr().IsGlobalUnicast() || prefix.Bits() >= 128 {
		return netip.Prefix{}, netip.Addr{}, nil, errors.New("manifest prefix must be global IPv6 with host space")
	}
	prefix = prefix.Masked()
	gateway, err := netip.ParseAddr(manifest.Gateway)
	if err != nil || !gateway.Is6() || gateway.IsUnspecified() || gateway.IsMulticast() || gateway.IsLoopback() {
		return netip.Prefix{}, netip.Addr{}, nil, errors.New("manifest gateway must be usable IPv6")
	}
	if len(manifest.Addresses) == 0 || len(manifest.Addresses) > 256 {
		return netip.Prefix{}, netip.Addr{}, nil, errors.New("manifest must contain between 1 and 256 IPv6 addresses")
	}
	addresses := make([]netip.Addr, 0, len(manifest.Addresses))
	seen := make(map[netip.Addr]struct{}, len(manifest.Addresses))
	for _, value := range manifest.Addresses {
		address, parseErr := netip.ParseAddr(value)
		if parseErr != nil || !address.Is6() || !address.IsGlobalUnicast() || !prefix.Contains(address) {
			return netip.Prefix{}, netip.Addr{}, nil, fmt.Errorf("manifest address %q is outside the global prefix", value)
		}
		address = address.Unmap()
		if _, duplicate := seen[address]; duplicate {
			return netip.Prefix{}, netip.Addr{}, nil, fmt.Errorf("duplicate manifest address %q", address)
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address)
	}
	return prefix, gateway, addresses, nil
}

func (manifest Manifest) firewallInput() projectnetwork.FirewallInput {
	ports := make([]projectnetwork.PortRule, 0, len(manifest.Firewall.NodePorts))
	for _, port := range manifest.Firewall.NodePorts {
		ports = append(ports, projectnetwork.PortRule{Port: port.Port, Protocol: port.Protocol})
	}
	return projectnetwork.FirewallInput{
		Backend:      manifest.Firewall.Backend,
		PanelPort:    manifest.Firewall.PanelPort,
		AllowedCIDRs: append([]string(nil), manifest.Firewall.AllowedCIDRs...),
		NodePorts:    ports,
	}
}

func readProtectedManifest(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, errors.New("integration manifest must be a regular file with mode 0600")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm() != 0o600 {
		return nil, errors.New("integration manifest changed while opening")
	}
	payload, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read integration manifest: %w", err)
	}
	return payload, nil
}

func stageManifestFile(directory, pattern string, payload []byte) (string, error) {
	file, err := os.CreateTemp(directory, "."+pattern+".*")
	if err != nil {
		return "", fmt.Errorf("create manifest temporary file: %w", err)
	}
	path := file.Name()
	cleanup := func(result error) (string, error) {
		file.Close()
		os.Remove(path)
		return "", result
	}
	if err := file.Chmod(0o600); err != nil {
		return cleanup(fmt.Errorf("protect manifest temporary file: %w", err))
	}
	if _, err := file.Write(payload); err != nil {
		return cleanup(fmt.Errorf("write manifest temporary file: %w", err))
	}
	if err := file.Sync(); err != nil {
		return cleanup(fmt.Errorf("sync manifest temporary file: %w", err))
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("close manifest temporary file: %w", err)
	}
	return path, nil
}

func replaceManifestFile(path string, payload []byte) error {
	directory := filepath.Dir(path)
	temporaryPath, err := stageManifestFile(directory, filepath.Base(path), payload)
	if err != nil {
		return err
	}
	defer os.Remove(temporaryPath)
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncManifestDirectory(directory)
}

func syncManifestDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open manifest directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync manifest directory: %w", err)
	}
	return nil
}

func ensureManifestEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("integration manifest contains multiple JSON values")
		}
		return fmt.Errorf("decode trailing integration manifest data: %w", err)
	}
	return nil
}

func cloneCommands(commands []projectnetwork.Command) []projectnetwork.Command {
	result := make([]projectnetwork.Command, len(commands))
	for index, command := range commands {
		result[index] = projectnetwork.Command{Name: command.Name, Args: append([]string(nil), command.Args...)}
	}
	return result
}
