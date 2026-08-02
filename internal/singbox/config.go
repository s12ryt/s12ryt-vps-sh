package singbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

var nodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var transportPathPattern = regexp.MustCompile(`^/[A-Za-z0-9/_-]{1,128}$`)
var serviceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
var realityShortIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{0,16}$`)

const (
	TransportWebSocket = "websocket"
	TransportGRPC      = "grpc"
)

type ServerInput struct {
	Nodes         []InboundNode
	IPv6Outbounds []netip.Addr
}

type InboundNode struct {
	ID         string
	Protocol   domain.Protocol
	Port       int
	Listeners  []netip.Addr
	Credential Credential
	TLS        TLSConfig
	Transport  TransportConfig
}

type Credential struct {
	Username string
	UUID     string
	Password string
	Method   string
}

type TLSConfig struct {
	Enabled         bool
	ServerName      string
	CertificatePath string
	KeyPath         string
	Reality         *RealityConfig
}

type RealityConfig struct {
	HandshakeServer string
	HandshakePort   int
	PrivateKey      string
	ShortID         string
}

type TransportConfig struct {
	Type        string
	Path        string
	ServiceName string
}

type serverConfig struct {
	Inbounds  []map[string]any `json:"inbounds"`
	Outbounds []map[string]any `json:"outbounds"`
}

func GenerateServerConfig(input ServerInput) ([]byte, error) {
	config := serverConfig{
		Inbounds:  make([]map[string]any, 0),
		Outbounds: make([]map[string]any, 0, len(input.IPv6Outbounds)),
	}
	seenNodes := make(map[string]struct{}, len(input.Nodes))
	seenListeners := make(map[string]struct{})
	for _, node := range input.Nodes {
		if err := validateInboundNode(node); err != nil {
			return nil, fmt.Errorf("node %q: %w", node.ID, err)
		}
		if _, duplicate := seenNodes[node.ID]; duplicate {
			return nil, fmt.Errorf("duplicate node ID %q", node.ID)
		}
		seenNodes[node.ID] = struct{}{}
		for _, listener := range node.Listeners {
			key := fmt.Sprintf("%s:%d", listener, node.Port)
			if _, duplicate := seenListeners[key]; duplicate {
				return nil, fmt.Errorf("duplicate listener %s", key)
			}
			seenListeners[key] = struct{}{}
			inbound, err := buildInbound(node, listener)
			if err != nil {
				return nil, fmt.Errorf("node %q: %w", node.ID, err)
			}
			config.Inbounds = append(config.Inbounds, inbound)
		}
	}

	seenOutbounds := make(map[netip.Addr]struct{}, len(input.IPv6Outbounds))
	for index, address := range input.IPv6Outbounds {
		if !address.Is6() || !address.IsGlobalUnicast() {
			return nil, fmt.Errorf("IPv6 outbound %d is not a global IPv6 address", index+1)
		}
		if _, duplicate := seenOutbounds[address]; duplicate {
			return nil, fmt.Errorf("duplicate IPv6 outbound %s", address)
		}
		seenOutbounds[address] = struct{}{}
		config.Outbounds = append(config.Outbounds, map[string]any{
			"type":               "direct",
			"tag":                fmt.Sprintf("direct-v6-%d", index+1),
			"inet6_bind_address": address.String(),
		})
	}

	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal sing-box configuration: %w", err)
	}
	return payload, nil
}

func validateInboundNode(node InboundNode) error {
	if !nodeIDPattern.MatchString(node.ID) {
		return errors.New("invalid node ID")
	}
	if node.Port < 20000 || node.Port > 49999 {
		return errors.New("port is outside 20000-49999")
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
	if err := validateCredential(node.Protocol, node.Credential); err != nil {
		return err
	}
	if err := validateTransport(node); err != nil {
		return err
	}
	if err := validateTLS(node); err != nil {
		return err
	}
	return nil
}

func validateTransport(node InboundNode) error {
	transport := node.Transport
	if transport.Type == "" {
		return nil
	}
	if node.Protocol != domain.ProtocolVLESS && node.Protocol != domain.ProtocolVMess {
		return errors.New("transport is only supported for VLESS and VMess")
	}
	if !node.TLS.Enabled {
		return errors.New("configured transport requires TLS")
	}
	switch transport.Type {
	case TransportWebSocket:
		if !transportPathPattern.MatchString(transport.Path) || strings.Contains(transport.Path, "..") {
			return errors.New("invalid WebSocket path")
		}
		if transport.ServiceName != "" {
			return errors.New("WebSocket transport cannot use a gRPC service name")
		}
	case TransportGRPC:
		if !serviceNamePattern.MatchString(transport.ServiceName) {
			return errors.New("invalid gRPC service name")
		}
		if transport.Path != "" {
			return errors.New("gRPC transport cannot use a WebSocket path")
		}
	default:
		return fmt.Errorf("unsupported transport %q", transport.Type)
	}
	return nil
}

func validateTLS(node InboundNode) error {
	tls := node.TLS
	if tls.Reality != nil {
		if !tls.Enabled || node.Protocol != domain.ProtocolVLESS || node.Transport.Type != "" {
			return errors.New("Reality requires VLESS over TCP with TLS enabled")
		}
		reality := tls.Reality
		if reality.HandshakeServer == "" || strings.ContainsAny(reality.HandshakeServer, " /\\") {
			return errors.New("invalid Reality handshake server")
		}
		if reality.HandshakePort < 1 || reality.HandshakePort > 65535 {
			return errors.New("invalid Reality handshake port")
		}
		if reality.PrivateKey == "" || !realityShortIDPattern.MatchString(reality.ShortID) {
			return errors.New("Reality private key and hexadecimal short ID are required")
		}
		return nil
	}
	if tls.Enabled && (tls.CertificatePath == "" || tls.KeyPath == "") {
		return errors.New("TLS certificate and key paths are required")
	}
	return nil
}

func validateCredential(protocol domain.Protocol, credential Credential) error {
	switch protocol {
	case domain.ProtocolVLESS, domain.ProtocolVMess:
		if !uuidPattern.MatchString(credential.UUID) {
			return errors.New("valid UUID credential is required")
		}
	case domain.ProtocolTUIC:
		if !uuidPattern.MatchString(credential.UUID) || credential.Password == "" {
			return errors.New("TUIC UUID and password are required")
		}
	case domain.ProtocolHysteria2, domain.ProtocolSOCKS5, domain.ProtocolAnyTLS:
		if credential.Password == "" {
			return errors.New("password credential is required")
		}
	case domain.ProtocolShadowsocks:
		if credential.Method == "" || credential.Password == "" {
			return errors.New("Shadowsocks method and password are required")
		}
	default:
		return fmt.Errorf("unsupported protocol %q", protocol)
	}
	return nil
}

func buildInbound(node InboundNode, listener netip.Addr) (map[string]any, error) {
	tagFamily := "v6"
	if listener.Is4() {
		tagFamily = "v4"
	}
	inbound := map[string]any{
		"type":        inboundType(node.Protocol),
		"tag":         fmt.Sprintf("in-%s-%s", node.ID, tagFamily),
		"listen":      listener.String(),
		"listen_port": node.Port,
	}

	credential := node.Credential
	switch node.Protocol {
	case domain.ProtocolVLESS, domain.ProtocolVMess:
		inbound["users"] = []map[string]any{{"name": credential.Username, "uuid": credential.UUID}}
	case domain.ProtocolTUIC:
		inbound["users"] = []map[string]any{{"name": credential.Username, "uuid": credential.UUID, "password": credential.Password}}
	case domain.ProtocolHysteria2, domain.ProtocolAnyTLS:
		inbound["users"] = []map[string]any{{"name": credential.Username, "password": credential.Password}}
	case domain.ProtocolSOCKS5:
		inbound["users"] = []map[string]any{{"username": credential.Username, "password": credential.Password}}
	case domain.ProtocolShadowsocks:
		inbound["method"] = credential.Method
		inbound["password"] = credential.Password
	default:
		return nil, fmt.Errorf("unsupported protocol %q", node.Protocol)
	}
	if node.TLS.Enabled {
		tls := map[string]any{
			"enabled":          true,
			"certificate_path": node.TLS.CertificatePath,
			"key_path":         node.TLS.KeyPath,
		}
		if node.TLS.ServerName != "" {
			tls["server_name"] = node.TLS.ServerName
		}
		if reality := node.TLS.Reality; reality != nil {
			delete(tls, "certificate_path")
			delete(tls, "key_path")
			tls["reality"] = map[string]any{
				"enabled": true,
				"handshake": map[string]any{
					"server":      reality.HandshakeServer,
					"server_port": reality.HandshakePort,
				},
				"private_key": reality.PrivateKey,
				"short_id":    []string{reality.ShortID},
			}
		}
		inbound["tls"] = tls
	}
	if node.Transport.Type != "" {
		transport := map[string]any{}
		switch node.Transport.Type {
		case TransportWebSocket:
			transport["type"] = "ws"
			transport["path"] = node.Transport.Path
		case TransportGRPC:
			transport["type"] = "grpc"
			transport["service_name"] = node.Transport.ServiceName
		}
		inbound["transport"] = transport
	}
	return inbound, nil
}

func inboundType(protocol domain.Protocol) string {
	if protocol == domain.ProtocolSOCKS5 {
		return "socks"
	}
	return string(protocol)
}
