package runtimeconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/importer"
	"github.com/s12ryt/s12ryt-vps-sh/internal/singbox"
)

const DeploymentStateSchemaVersion = 1

var persistedNodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
var persistedTransportPathPattern = regexp.MustCompile(`^/[A-Za-z0-9/_-]{1,128}$`)
var persistedServiceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
var persistedRealityShortIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{0,16}$`)

type DeploymentState struct {
	SchemaVersion   int                       `json:"schema_version"`
	Nodes           []PersistedNodeDeployment `json:"nodes"`
	IPv6Outbounds   []netip.Addr              `json:"ipv6_outbounds"`
	RemoteOutbounds []PersistedRemoteOutbound `json:"remote_outbounds,omitempty"`
	IPv4Fallback    []string                  `json:"ipv4_fallback,omitempty"`
}

type PersistedRemoteOutbound struct {
	Enabled bool            `json:"enabled"`
	Config  json.RawMessage `json:"config"`
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
	ACME            *PersistedACMEConfig    `json:"acme,omitempty"`
}

type PersistedRealityConfig struct {
	HandshakeServer string `json:"handshake_server"`
	HandshakePort   int    `json:"handshake_port"`
	PrivateKey      string `json:"private_key"`
	ShortID         string `json:"short_id"`
}

type PersistedACMEConfig struct {
	Domains           []string `json:"domains"`
	DataDirectory     string   `json:"data_directory"`
	DefaultServerName string   `json:"default_server_name"`
	Email             string   `json:"email,omitempty"`
	Provider          string   `json:"provider"`
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
	_, remoteTypes, err := state.parseRemoteOutbounds()
	if err != nil {
		return err
	}
	seenFallback := make(map[string]struct{}, len(state.IPv4Fallback))
	for _, tag := range state.IPv4Fallback {
		if !persistedNodeIDPattern.MatchString(tag) {
			return fmt.Errorf("unsafe IPv4 fallback tag %q", tag)
		}
		if _, duplicate := seenFallback[tag]; duplicate {
			return fmt.Errorf("duplicate IPv4 fallback %q", tag)
		}
		seenFallback[tag] = struct{}{}
		if tag == "direct-v4" {
			continue
		}
		typeName, exists := remoteTypes[tag]
		if !exists {
			return fmt.Errorf("IPv4 fallback references unknown outbound %q", tag)
		}
		if typeName != "socks" && typeName != "http" {
			return fmt.Errorf("IPv4 fallback %q is not a SOCKS or HTTP outbound", tag)
		}
	}
	return nil
}

func (state DeploymentState) parseRemoteOutbounds() ([]RemoteOutbound, map[string]string, error) {
	result := make([]RemoteOutbound, 0, len(state.RemoteOutbounds))
	types := make(map[string]string, len(state.RemoteOutbounds))
	for index, persisted := range state.RemoteOutbounds {
		if len(persisted.Config) == 0 {
			return nil, nil, fmt.Errorf("remote outbound %d configuration is empty", index+1)
		}
		var declared struct {
			Tag string `json:"tag"`
		}
		if err := json.Unmarshal(persisted.Config, &declared); err != nil {
			return nil, nil, fmt.Errorf("remote outbound %d configuration is invalid: %w", index+1, err)
		}
		if !persistedNodeIDPattern.MatchString(declared.Tag) || reservedRemoteOutboundTag(declared.Tag) {
			return nil, nil, fmt.Errorf("unsafe remote outbound tag %q", declared.Tag)
		}
		imported, err := importer.Import(persisted.Config, importer.Options{AllowIPv4Proxy: true})
		if err != nil {
			return nil, nil, fmt.Errorf("validate remote outbound %q: %w", declared.Tag, err)
		}
		if len(imported) != 1 || imported[0].Tag != declared.Tag {
			return nil, nil, fmt.Errorf("remote outbound %q must contain one matching canonical configuration", declared.Tag)
		}
		if _, duplicate := types[declared.Tag]; duplicate {
			return nil, nil, fmt.Errorf("duplicate remote outbound tag %q", declared.Tag)
		}
		types[declared.Tag] = imported[0].Type
		result = append(result, RemoteOutbound{
			Tag:     declared.Tag,
			Type:    imported[0].Type,
			Enabled: persisted.Enabled,
			Config:  cloneOutboundMap(imported[0].Raw),
		})
	}
	return result, types, nil
}

func reservedRemoteOutboundTag(tag string) bool {
	return tag == "direct-v4" || tag == "select-ipv4" || strings.HasPrefix(tag, "direct-v6-") || strings.HasPrefix(tag, "rotate-")
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
	if config.Reality != nil && config.ACME != nil {
		return errors.New("Reality and ACME cannot be enabled together")
	}
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
	if config.ACME != nil {
		if !config.Enabled {
			return errors.New("ACME requires TLS to be enabled")
		}
		if config.CertificatePath != "" || config.KeyPath != "" {
			return errors.New("ACME cannot use certificate and key paths")
		}
		if err := config.ACME.validate(); err != nil {
			return err
		}
		if config.ServerName == "" || !persistedContainsString(config.ACME.Domains, config.ServerName) {
			return errors.New("TLS server name must be one of the ACME domains")
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

func (config PersistedACMEConfig) validate() error {
	if len(config.Domains) == 0 {
		return errors.New("ACME requires at least one domain")
	}
	seen := make(map[string]struct{}, len(config.Domains))
	for _, domainName := range config.Domains {
		if !validPersistedACMEDomain(domainName) {
			return fmt.Errorf("invalid ACME domain %q", domainName)
		}
		if _, duplicate := seen[domainName]; duplicate {
			return fmt.Errorf("duplicate ACME domain %q", domainName)
		}
		seen[domainName] = struct{}{}
	}
	if config.DataDirectory != singbox.ACMEDataDirectory {
		return fmt.Errorf("ACME data directory must be %s", singbox.ACMEDataDirectory)
	}
	if !persistedContainsString(config.Domains, config.DefaultServerName) {
		return errors.New("ACME default server name must be one of the ACME domains")
	}
	if config.Provider != "letsencrypt" {
		return errors.New("ACME provider must be letsencrypt")
	}
	if config.Email != "" {
		address, err := mail.ParseAddress(config.Email)
		if err != nil || address.Address != config.Email {
			return errors.New("invalid ACME account email")
		}
	}
	return nil
}

func validPersistedACMEDomain(value string) bool {
	if len(value) < 3 || len(value) > 253 || strings.Contains(value, "..") || !strings.Contains(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || !persistedASCIIAlphaNumeric(label[0]) || !persistedASCIIAlphaNumeric(label[len(label)-1]) {
			return false
		}
		for index := 1; index < len(label)-1; index++ {
			if !persistedASCIIAlphaNumeric(label[index]) && label[index] != '-' {
				return false
			}
		}
	}
	return true
}

func persistedASCIIAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func persistedContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
	remoteOutbounds, _, err := state.parseRemoteOutbounds()
	if err != nil {
		return Input{}, fmt.Errorf("resolve remote outbounds: %w", err)
	}
	input := Input{
		Config:          config,
		Deployments:     make([]NodeDeployment, 0, len(state.Nodes)),
		IPv6Outbounds:   append([]netip.Addr(nil), state.IPv6Outbounds...),
		RemoteOutbounds: remoteOutbounds,
		IPv4Fallback:    append([]string(nil), state.IPv4Fallback...),
	}
	for _, persisted := range state.Nodes {
		input.Deployments = append(input.Deployments, persisted.toRuntimeDeployment())
	}
	if _, err := CompileServerConfig(input); err != nil {
		return Input{}, fmt.Errorf("resolve deployment state: %w", err)
	}
	return input, nil
}

func cloneOutboundMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
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
	if node.TLS.ACME != nil {
		tlsConfig.ACME = &singbox.ACMEConfig{
			Domains:           append([]string(nil), node.TLS.ACME.Domains...),
			DataDirectory:     node.TLS.ACME.DataDirectory,
			DefaultServerName: node.TLS.ACME.DefaultServerName,
			Email:             node.TLS.ACME.Email,
			Provider:          node.TLS.ACME.Provider,
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
	pathInfo, err := os.Lstat(store.path)
	if err != nil {
		return DeploymentState{}, fmt.Errorf("inspect deployment state: %w", err)
	}
	if !pathInfo.Mode().IsRegular() {
		return DeploymentState{}, errors.New("deployment state must be a regular file")
	}
	if pathInfo.Mode().Perm() != 0o600 {
		return DeploymentState{}, fmt.Errorf("deployment state permissions must be 0600, got %04o", pathInfo.Mode().Perm())
	}
	file, err := os.Open(store.path)
	if err != nil {
		return DeploymentState{}, fmt.Errorf("open deployment state: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return DeploymentState{}, fmt.Errorf("inspect opened deployment state: %w", err)
	}
	if !openedInfo.Mode().IsRegular() || openedInfo.Mode().Perm() != 0o600 || !os.SameFile(pathInfo, openedInfo) {
		return DeploymentState{}, errors.New("deployment state changed during protected open")
	}
	data, err := io.ReadAll(file)
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
