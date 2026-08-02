package domain

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"regexp"
	"time"
)

const SchemaVersion = 1
const defaultHealthURL = "https://cloudflare.com/cdn-cgi/trace"
const credentialAlphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

var ErrInsufficientEntropy = errors.New("insufficient entropy")
var webPathPattern = regexp.MustCompile(`^/[A-Za-z0-9_-]{1,64}$`)
var nodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

type Protocol string

const ProtocolVLESS Protocol = "vless"
const ProtocolVMess Protocol = "vmess"
const ProtocolHysteria2 Protocol = "hysteria2"
const ProtocolTUIC Protocol = "tuic"
const ProtocolSOCKS5 Protocol = "socks5"
const ProtocolAnyTLS Protocol = "anytls"
const ProtocolShadowsocks Protocol = "shadowsocks"

type RoutingMode string

const RoutingModeClientIPv4 RoutingMode = "client-ipv4"
const RoutingModeVPSIPv4 RoutingMode = "vps-ipv4"
const RoutingModeIPv6Only RoutingMode = "ipv6-only"

type Topology string

const TopologyMultiIPv6MultiNode Topology = "multi-ipv6-multi-node"
const TopologySingleIPv6SingleNode Topology = "single-ipv6-single-node"
const TopologyMultiIPv6RotatingNode Topology = "multi-ipv6-rotating-node"
const TopologyMultiIPv6RotatingNodes Topology = "multi-ipv6-rotating-nodes"

type Config struct {
	SchemaVersion int            `json:"schema_version"`
	Panel         PanelConfig    `json:"panel"`
	IPv6          IPv6PoolConfig `json:"ipv6"`
	Routing       RoutingConfig  `json:"routing"`
	Health        HealthConfig   `json:"health"`
	Nodes         []Node         `json:"nodes"`
}

type PanelConfig struct {
	Port         int      `json:"port"`
	Path         string   `json:"path"`
	AllowedCIDRs []string `json:"allowed_cidrs"`
}

type IPv6PoolConfig struct {
	GeneratedCount int `json:"generated_count"`
}

type RoutingConfig struct {
	Mode             RoutingMode   `json:"mode"`
	Topology         Topology      `json:"topology"`
	RotationInterval time.Duration `json:"rotation_interval"`
}

type HealthConfig struct {
	URL               string        `json:"url"`
	Interval          time.Duration `json:"interval"`
	Timeout           time.Duration `json:"timeout"`
	FailureThreshold  int           `json:"failure_threshold"`
	RecoveryThreshold int           `json:"recovery_threshold"`
}

type Node struct {
	ID         string         `json:"id"`
	Protocol   Protocol       `json:"protocol"`
	Port       int            `json:"port"`
	Enabled    bool           `json:"enabled"`
	Credential NodeCredential `json:"credential"`
}

type BootstrapSecrets struct {
	Password string
	WebPath  string
}

func DefaultConfig() Config {
	return Config{
		SchemaVersion: SchemaVersion,
		Panel: PanelConfig{
			Port:         34456,
			Path:         "/configureme1",
			AllowedCIDRs: []string{"0.0.0.0/0", "::/0"},
		},
		IPv6: IPv6PoolConfig{GeneratedCount: 16},
		Routing: RoutingConfig{
			Mode:             RoutingModeVPSIPv4,
			Topology:         TopologyMultiIPv6MultiNode,
			RotationInterval: time.Hour,
		},
		Health: HealthConfig{
			URL:               defaultHealthURL,
			Interval:          30 * time.Second,
			Timeout:           5 * time.Second,
			FailureThreshold:  3,
			RecoveryThreshold: 3,
		},
	}
}

func GenerateBootstrapSecrets(reader io.Reader) (BootstrapSecrets, error) {
	password, err := randomAlphaNumeric(reader, 24)
	if err != nil {
		return BootstrapSecrets{}, err
	}
	path, err := randomAlphaNumeric(reader, 12)
	if err != nil {
		return BootstrapSecrets{}, err
	}
	return BootstrapSecrets{Password: password, WebPath: "/" + path}, nil
}

func randomAlphaNumeric(reader io.Reader, length int) (string, error) {
	if reader == nil {
		return "", ErrInsufficientEntropy
	}

	result := make([]byte, 0, length)
	buffer := make([]byte, 1)
	limit := byte(248)
	for len(result) < length {
		if _, err := io.ReadFull(reader, buffer); err != nil {
			return "", fmt.Errorf("%w: %v", ErrInsufficientEntropy, err)
		}
		if buffer[0] >= limit {
			continue
		}
		result = append(result, credentialAlphabet[int(buffer[0])%len(credentialAlphabet)])
	}
	return string(result), nil
}

func (config Config) Validate() error {
	if config.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema version %d", config.SchemaVersion)
	}
	if config.Panel.Port < 1 || config.Panel.Port > 65535 {
		return fmt.Errorf("panel port %d is outside 1-65535", config.Panel.Port)
	}
	if !webPathPattern.MatchString(config.Panel.Path) {
		return errors.New("panel path must be one URL-safe segment")
	}
	for _, cidr := range config.Panel.AllowedCIDRs {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			return fmt.Errorf("invalid panel CIDR %q: %w", cidr, err)
		}
	}
	if config.IPv6.GeneratedCount < 1 || config.IPv6.GeneratedCount > 256 {
		return fmt.Errorf("generated IPv6 count %d is outside 1-256", config.IPv6.GeneratedCount)
	}
	if !supportedRoutingMode(config.Routing.Mode) {
		return fmt.Errorf("unsupported routing mode %q", config.Routing.Mode)
	}
	if !supportedTopology(config.Routing.Topology) {
		return fmt.Errorf("unsupported topology %q", config.Routing.Topology)
	}
	if config.Routing.RotationInterval <= 0 {
		return errors.New("rotation interval must be positive")
	}
	if err := config.Health.validate(); err != nil {
		return err
	}

	nodeIDs := make(map[string]struct{}, len(config.Nodes))
	for index, node := range config.Nodes {
		if !nodeIDPattern.MatchString(node.ID) {
			return fmt.Errorf("node %d has invalid ID %q", index, node.ID)
		}
		if _, exists := nodeIDs[node.ID]; exists {
			return fmt.Errorf("duplicate node ID %q", node.ID)
		}
		nodeIDs[node.ID] = struct{}{}
		if !supportedProtocol(node.Protocol) {
			return fmt.Errorf("node %q uses unsupported protocol %q", node.ID, node.Protocol)
		}
		if node.Port < 20000 || node.Port > 49999 {
			return fmt.Errorf("node %q port %d is outside 20000-49999", node.ID, node.Port)
		}
		if err := node.Credential.Validate(node.Protocol); err != nil {
			return fmt.Errorf("node %q credential is invalid: %w", node.ID, err)
		}
	}
	return nil
}

func (health HealthConfig) validate() error {
	parsed, err := url.Parse(health.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("health URL must be an absolute HTTPS URL")
	}
	if health.Interval <= 0 || health.Timeout <= 0 {
		return errors.New("health interval and timeout must be positive")
	}
	if health.FailureThreshold <= 0 || health.RecoveryThreshold <= 0 {
		return errors.New("health thresholds must be positive")
	}
	return nil
}

func supportedProtocol(protocol Protocol) bool {
	switch protocol {
	case ProtocolVLESS, ProtocolVMess, ProtocolHysteria2, ProtocolTUIC,
		ProtocolSOCKS5, ProtocolAnyTLS, ProtocolShadowsocks:
		return true
	default:
		return false
	}
}

func supportedRoutingMode(mode RoutingMode) bool {
	return mode == RoutingModeClientIPv4 || mode == RoutingModeVPSIPv4 || mode == RoutingModeIPv6Only
}

func supportedTopology(topology Topology) bool {
	switch topology {
	case TopologyMultiIPv6MultiNode, TopologySingleIPv6SingleNode,
		TopologyMultiIPv6RotatingNode, TopologyMultiIPv6RotatingNodes:
		return true
	default:
		return false
	}
}
