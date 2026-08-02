package singbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/topology"
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
	Nodes           []InboundNode
	IPv6Outbounds   []netip.Addr
	RemoteOutbounds []map[string]any
	RoutingPlan     *topology.Plan
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
	Route     *routeConfig     `json:"route,omitempty"`
}

type routeConfig struct {
	Rules []map[string]any `json:"rules"`
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
	if err := appendRemoteOutbounds(&config, input.RemoteOutbounds); err != nil {
		return nil, fmt.Errorf("append remote outbounds: %w", err)
	}
	if input.RoutingPlan != nil {
		if err := applyRoutingPlan(&config, input.Nodes, *input.RoutingPlan); err != nil {
			return nil, fmt.Errorf("apply routing plan: %w", err)
		}
	}

	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal sing-box configuration: %w", err)
	}
	return payload, nil
}

func appendRemoteOutbounds(config *serverConfig, input []map[string]any) error {
	available := outboundTags(config.Outbounds)
	for index, source := range input {
		outbound, err := cloneRemoteOutbound(source)
		if err != nil {
			return fmt.Errorf("remote outbound %d: %w", index+1, err)
		}
		typeName, _ := outbound["type"].(string)
		if !supportedRemoteOutboundType(typeName) {
			return fmt.Errorf("remote outbound %d has unsupported type %q", index+1, typeName)
		}
		tag, _ := outbound["tag"].(string)
		if !nodeIDPattern.MatchString(tag) {
			return fmt.Errorf("remote outbound %d has an unsafe tag", index+1)
		}
		if _, duplicate := available[tag]; duplicate {
			return fmt.Errorf("remote outbound tag %q conflicts with another outbound", tag)
		}
		server, _ := outbound["server"].(string)
		if strings.TrimSpace(server) == "" {
			return fmt.Errorf("remote outbound %q requires a server", tag)
		}
		if !validRemoteServerPort(outbound["server_port"]) {
			return fmt.Errorf("remote outbound %q has an invalid server port", tag)
		}
		available[tag] = struct{}{}
		config.Outbounds = append(config.Outbounds, outbound)
	}
	return nil
}

func cloneRemoteOutbound(input map[string]any) (map[string]any, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode configuration: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	var output map[string]any
	if err := decoder.Decode(&output); err != nil {
		return nil, fmt.Errorf("decode configuration: %w", err)
	}
	return output, nil
}

func supportedRemoteOutboundType(typeName string) bool {
	switch typeName {
	case "vless", "vmess", "hysteria2", "tuic", "socks", "anytls", "shadowsocks", "http":
		return true
	default:
		return false
	}
}

func validRemoteServerPort(value any) bool {
	var port int64
	switch typed := value.(type) {
	case int:
		port = int64(typed)
	case int64:
		port = typed
	case float64:
		port = int64(typed)
		if float64(port) != typed {
			return false
		}
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return false
		}
		port = parsed
	default:
		return false
	}
	return port >= 1 && port <= 65535
}

func applyRoutingPlan(config *serverConfig, nodes []InboundNode, plan topology.Plan) error {
	nodeTags := make(map[string][]string, len(nodes))
	for _, node := range nodes {
		if _, duplicate := nodeTags[node.ID]; duplicate {
			return fmt.Errorf("duplicate routing node %q", node.ID)
		}
		tags := make([]string, 0, len(node.Listeners))
		for _, listener := range node.Listeners {
			family := "v6"
			if listener.Is4() {
				family = "v4"
			}
			tags = append(tags, fmt.Sprintf("in-%s-%s", node.ID, family))
		}
		nodeTags[node.ID] = tags
	}

	available := outboundTags(config.Outbounds)
	ipv4Target, err := appendIPv4RoutingOutbounds(config, available, plan)
	if err != nil {
		return err
	}
	rules := make([]map[string]any, 0, len(plan.Nodes)*2)
	seenPlans := make(map[string]struct{}, len(plan.Nodes))
	for _, nodePlan := range plan.Nodes {
		inboundTags, exists := nodeTags[nodePlan.NodeID]
		if !exists {
			return fmt.Errorf("routing plan references unknown node %q", nodePlan.NodeID)
		}
		if _, duplicate := seenPlans[nodePlan.NodeID]; duplicate {
			return fmt.Errorf("duplicate routing plan for node %q", nodePlan.NodeID)
		}
		seenPlans[nodePlan.NodeID] = struct{}{}
		outbound, err := appendNodeRoutingOutbound(config, available, nodePlan)
		if err != nil {
			return err
		}
		rules = append(rules, map[string]any{
			"inbound":    inboundTags,
			"ip_version": 6,
			"action":     "route",
			"outbound":   outbound,
		})
		ipv4Rule := map[string]any{
			"inbound":    inboundTags,
			"ip_version": 4,
		}
		if ipv4Target == "" {
			ipv4Rule["action"] = "reject"
		} else {
			ipv4Rule["action"] = "route"
			ipv4Rule["outbound"] = ipv4Target
		}
		rules = append(rules, ipv4Rule)
	}
	if len(seenPlans) != len(nodeTags) {
		return errors.New("routing plan must cover every enabled node")
	}
	config.Route = &routeConfig{Rules: rules}
	return nil
}

func outboundTags(outbounds []map[string]any) map[string]struct{} {
	tags := make(map[string]struct{}, len(outbounds))
	for _, outbound := range outbounds {
		if tag, ok := outbound["tag"].(string); ok {
			tags[tag] = struct{}{}
		}
	}
	return tags
}

func appendIPv4RoutingOutbounds(config *serverConfig, available map[string]struct{}, plan topology.Plan) (string, error) {
	switch plan.Mode {
	case domain.RoutingModeClientIPv4:
		if plan.IPv4.Action != topology.IPv4ClientDirect || len(plan.IPv4.Candidates) != 0 {
			return "", errors.New("client IPv4 mode has an invalid IPv4 policy")
		}
		return "", nil
	case domain.RoutingModeIPv6Only:
		if plan.IPv4.Action != topology.IPv4Reject || len(plan.IPv4.Candidates) != 0 {
			return "", errors.New("IPv6-only mode has an invalid IPv4 policy")
		}
		return "", nil
	case domain.RoutingModeVPSIPv4:
		if plan.IPv4.Action != topology.IPv4VPSFallback || len(plan.IPv4.Candidates) == 0 {
			return "", errors.New("VPS IPv4 mode requires ordered IPv4 outbounds")
		}
	default:
		return "", fmt.Errorf("unsupported routing mode %q", plan.Mode)
	}

	for _, candidate := range plan.IPv4.Candidates {
		if candidate == "direct-v4" {
			if _, exists := available[candidate]; !exists {
				config.Outbounds = append(config.Outbounds, map[string]any{"type": "direct", "tag": candidate})
				available[candidate] = struct{}{}
			}
		}
		if _, exists := available[candidate]; !exists {
			return "", fmt.Errorf("IPv4 routing references unavailable outbound %q", candidate)
		}
	}
	if len(plan.IPv4.Candidates) == 1 {
		return plan.IPv4.Candidates[0], nil
	}
	const selectorTag = "select-ipv4"
	if _, exists := available[selectorTag]; exists {
		return "", fmt.Errorf("IPv4 selector tag %q conflicts with an outbound", selectorTag)
	}
	config.Outbounds = append(config.Outbounds, selectorOutbound(selectorTag, plan.IPv4.Candidates, 0))
	available[selectorTag] = struct{}{}
	return selectorTag, nil
}

func appendNodeRoutingOutbound(
	config *serverConfig,
	available map[string]struct{},
	plan topology.NodePlan,
) (string, error) {
	if plan.StaticOutbound != "" && plan.Rotation != nil {
		return "", fmt.Errorf("node %q cannot use static and rotating outbounds together", plan.NodeID)
	}
	if plan.StaticOutbound != "" {
		if _, exists := available[plan.StaticOutbound]; !exists {
			return "", fmt.Errorf("node %q references unavailable outbound %q", plan.NodeID, plan.StaticOutbound)
		}
		return plan.StaticOutbound, nil
	}
	rotation := plan.Rotation
	if rotation == nil || len(rotation.Candidates) < 2 || rotation.Interval <= 0 || !rotation.NewConnectionsOnly {
		return "", fmt.Errorf("node %q has an invalid rotation plan", plan.NodeID)
	}
	if rotation.StartIndex < 0 || rotation.StartIndex >= len(rotation.Candidates) {
		return "", fmt.Errorf("node %q rotation start index is invalid", plan.NodeID)
	}
	candidates := make([]string, 0, len(rotation.Candidates))
	seen := make(map[string]struct{}, len(rotation.Candidates))
	for _, candidate := range rotation.Candidates {
		if _, duplicate := seen[candidate.Tag]; duplicate {
			return "", fmt.Errorf("node %q has duplicate rotation outbound %q", plan.NodeID, candidate.Tag)
		}
		if _, exists := available[candidate.Tag]; !exists {
			return "", fmt.Errorf("node %q references unavailable outbound %q", plan.NodeID, candidate.Tag)
		}
		seen[candidate.Tag] = struct{}{}
		candidates = append(candidates, candidate.Tag)
	}
	selectorTag := "rotate-" + plan.NodeID
	if _, exists := available[selectorTag]; exists {
		return "", fmt.Errorf("rotation selector tag %q conflicts with an outbound", selectorTag)
	}
	config.Outbounds = append(config.Outbounds, selectorOutbound(selectorTag, candidates, rotation.StartIndex))
	available[selectorTag] = struct{}{}
	return selectorTag, nil
}

func selectorOutbound(tag string, candidates []string, defaultIndex int) map[string]any {
	return map[string]any{
		"type":                        "selector",
		"tag":                         tag,
		"outbounds":                   append([]string(nil), candidates...),
		"default":                     candidates[defaultIndex],
		"interrupt_exist_connections": false,
	}
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
