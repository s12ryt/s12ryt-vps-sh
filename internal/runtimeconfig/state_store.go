package runtimeconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/singbox"
)

const DeploymentStateSchemaVersion = 1

var persistedNodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
var persistedTransportPathPattern = regexp.MustCompile(`^/[A-Za-z0-9/_-]{1,128}$`)
var persistedServiceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
var persistedRealityShortIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{0,16}$`)

type DeploymentState struct {
	SchemaVersion int                       `json:"schema_version"`
	Nodes         []PersistedNodeDeployment `json:"nodes"`
	IPv6Outbounds []netip.Addr              `json:"ipv6_outbounds"`
}

type PersistedNodeDeployment struct {
	NodeID    string                   `json:"node_id"`
	Listeners []netip.Addr             `json:"listeners"`
	TLS       PersistedTLSConfig       `json:"tls"`
	Transport PersistedTransportConfig `json:"transport"`
}

type PersistedTLSConfig struct {
	Enabled         bool                    `json:"enabled"`
	ServerName      string                  `json:"server_name,omitempty"`
	CertificatePath string                  `json:"certificate_path,omitempty"`
	KeyPath         string                  `json:"key_path,omitempty"`
	Reality         *PersistedRealityConfig `json:"reality,omitempty"`
}

type PersistedRealityConfig struct {
	HandshakeServer string `json:"handshake_server"`
	HandshakePort   int    `json:"handshake_port"`
	PrivateKey      string `json:"private_key"`
	ShortID         string `json:"short_id"`
}

type PersistedTransportConfig struct {
	Type        string `json:"type,omitempty"`
	Path        string `json:"path,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
}

type DeploymentStateStore struct {
	path string
}

func NewDeploymentStateStore(path string) (*DeploymentStateStore, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("deployment state path must be absolute")
	}
	return &DeploymentStateStore{path: filepath.Clean(path)}, nil
}

func (state DeploymentState) Validate() error {
	if state.SchemaVersion != DeploymentStateSchemaVersion {
		return fmt.Errorf("unsupported deployment schema version %d", state.SchemaVersion)
	}
	seenNodes := make(map[string]struct{}, len(state.Nodes))
	for index, node := range state.Nodes {
		if err := node.validate(); err != nil {
			return fmt.Errorf("deployment node %d: %w", index, err)
		}
		if _, duplicate := seenNodes[node.NodeID]; duplicate {
			return fmt.Errorf("duplicate deployment node %q", node.NodeID)
		}
		seenNodes[node.NodeID] = struct{}{}
	}
	seenOutbounds := make(map[netip.Addr]struct{}, len(state.IPv6Outbounds))
	for index, address := range state.IPv6Outbounds {
		if !address.Is6() || !address.IsGlobalUnicast() {
			return fmt.Errorf("IPv6 outbound %d is not a global IPv6 address", index+1)
		}
		if _, duplicate := seenOutbounds[address]; duplicate {
			return fmt.Errorf("duplicate IPv6 outbound %s", address)
		}
		seenOutbounds[address] = struct{}{}
	}
	return nil
}

func (node PersistedNodeDeployment) validate() error {
	if !persistedNodeIDPattern.MatchString(node.NodeID) {
		return fmt.Errorf("invalid node ID %q", node.NodeID)
	}
	if len(node.Listeners) == 0 || len(node.Listeners) > 2 {
		return errors.New("one or two listeners are required")
	}
	families := make(map[bool]struct{}, len(node.Listeners))
	for _, listener := range node.Listeners {
		if !listener.IsValid() || !listener.IsGlobalUnicast() {
			return fmt.Errorf("listener %s is not globally routable", listener)
		}
		if _, duplicate := families[listener.Is4()]; duplicate {
			return errors.New("listeners must use distinct address families")
		}
		families[listener.Is4()] = struct{}{}
	}
	if err := node.TLS.validate(); err != nil {
		return err
	}
	if err := node.Transport.validate(); err != nil {
		return err
	}
	return nil
}

func (config PersistedTLSConfig) validate() error {
	if config.Reality != nil {
		if !config.Enabled {
			return errors.New("Reality requires TLS to be enabled")
		}
		if config.CertificatePath != "" || config.KeyPath != "" {
			return errors.New("Reality cannot use certificate and key paths")
		}
		reality := config.Reality
		if reality.HandshakeServer == "" || strings.ContainsAny(reality.HandshakeServer, " /\\") {
			return errors.New("invalid Reality handshake server")
		}
		if reality.HandshakePort < 1 || reality.HandshakePort > 65535 {
			return errors.New("invalid Reality handshake port")
		}
		if reality.PrivateKey == "" || !persistedRealityShortIDPattern.MatchString(reality.ShortID) {
			return errors.New("Reality private key and hexadecimal short ID are required")
		}
		return nil
	}
	if config.Enabled && (config.CertificatePath == "" || config.KeyPath == "") {
		return errors.New("TLS certificate and key paths are required")
	}
	if !config.Enabled && (config.ServerName != "" || config.CertificatePath != "" || config.KeyPath != "") {
		return errors.New("disabled TLS cannot contain certificate settings")
	}
	return nil
}

func (config PersistedTransportConfig) validate() error {
	switch config.Type {
	case "":
		if config.Path != "" || config.ServiceName != "" {
			return errors.New("transport fields require a transport type")
		}
	case singbox.TransportWebSocket:
		if !persistedTransportPathPattern.MatchString(config.Path) || strings.Contains(config.Path, "..") {
			return errors.New("invalid WebSocket path")
		}
		if config.ServiceName != "" {
			return errors.New("WebSocket transport cannot use a gRPC service name")
		}
	case singbox.TransportGRPC:
		if !persistedServiceNamePattern.MatchString(config.ServiceName) {
			return errors.New("invalid gRPC service name")
		}
		if config.Path != "" {
			return errors.New("gRPC transport cannot use a WebSocket path")
		}
	default:
		return fmt.Errorf("unsupported transport %q", config.Type)
	}
	return nil
}

func (state DeploymentState) Resolve(config domain.Config) (Input, error) {
	if err := state.Validate(); err != nil {
		return Input{}, fmt.Errorf("validate deployment state: %w", err)
	}
	input := Input{
		Config:        config,
		Deployments:   make([]NodeDeployment, 0, len(state.Nodes)),
		IPv6Outbounds: append([]netip.Addr(nil), state.IPv6Outbounds...),
	}
	for _, persisted := range state.Nodes {
		input.Deployments = append(input.Deployments, persisted.toRuntimeDeployment())
	}
	if _, err := CompileServerConfig(input); err != nil {
		return Input{}, fmt.Errorf("resolve deployment state: %w", err)
	}
	return input, nil
}

func (node PersistedNodeDeployment) toRuntimeDeployment() NodeDeployment {
	tlsConfig := singbox.TLSConfig{
		Enabled:         node.TLS.Enabled,
		ServerName:      node.TLS.ServerName,
		CertificatePath: node.TLS.CertificatePath,
		KeyPath:         node.TLS.KeyPath,
	}
	if node.TLS.Reality != nil {
		tlsConfig.Reality = &singbox.RealityConfig{
			HandshakeServer: node.TLS.Reality.HandshakeServer,
			HandshakePort:   node.TLS.Reality.HandshakePort,
			PrivateKey:      node.TLS.Reality.PrivateKey,
			ShortID:         node.TLS.Reality.ShortID,
		}
	}
	return NodeDeployment{
		NodeID:    node.NodeID,
		Listeners: append([]netip.Addr(nil), node.Listeners...),
		TLS:       tlsConfig,
		Transport: singbox.TransportConfig{
			Type:        node.Transport.Type,
			Path:        node.Transport.Path,
			ServiceName: node.Transport.ServiceName,
		},
	}
}

func (store *DeploymentStateStore) Load() (DeploymentState, error) {
	data, err := os.ReadFile(store.path)
	if err != nil {
		return DeploymentState{}, fmt.Errorf("read deployment state: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state DeploymentState
	if err := decoder.Decode(&state); err != nil {
		return DeploymentState{}, fmt.Errorf("decode deployment state: %w", err)
	}
	if err := ensureDeploymentJSONEnd(decoder); err != nil {
		return DeploymentState{}, err
	}
	if err := state.Validate(); err != nil {
		return DeploymentState{}, fmt.Errorf("validate deployment state: %w", err)
	}
	return state, nil
}

func ensureDeploymentJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode deployment state: multiple JSON values")
		}
		return fmt.Errorf("decode deployment state: %w", err)
	}
	return nil
}

func (store *DeploymentStateStore) Save(state DeploymentState) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("validate deployment state: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode deployment state: %w", err)
	}
	data = append(data, '\n')

	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create deployment state directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("secure deployment state directory: %w", err)
	}
	temporaryPath, err := writeDeploymentTemp(directory, ".runtime.*", data)
	if err != nil {
		return err
	}
	defer os.Remove(temporaryPath)
	if err := store.backupCurrent(directory); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		return fmt.Errorf("replace deployment state: %w", err)
	}
	if err := syncDeploymentDirectory(directory); err != nil {
		return fmt.Errorf("sync deployment state directory: %w", err)
	}
	return nil
}

func (store *DeploymentStateStore) backupCurrent(directory string) error {
	data, err := os.ReadFile(store.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read current deployment state: %w", err)
	}
	backupPath, err := writeDeploymentTemp(directory, ".runtime-backup.*", data)
	if err != nil {
		return err
	}
	defer os.Remove(backupPath)
	if err := os.Rename(backupPath, store.path+".bak"); err != nil {
		return fmt.Errorf("replace deployment state backup: %w", err)
	}
	return nil
}

func writeDeploymentTemp(directory, pattern string, data []byte) (string, error) {
	temporary, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return "", fmt.Errorf("create temporary deployment state: %w", err)
	}
	path := temporary.Name()
	failed := true
	defer func() {
		if failed {
			temporary.Close()
			os.Remove(path)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return "", fmt.Errorf("secure temporary deployment state: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return "", fmt.Errorf("write temporary deployment state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync temporary deployment state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close temporary deployment state: %w", err)
	}
	failed = false
	return path, nil
}

func syncDeploymentDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
