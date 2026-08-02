package importer

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

const MaxImportBytes = 1 << 20
const MaxNodes = 1000

type Options struct {
	AllowIPv4Proxy bool
}

type Outbound struct {
	Type   string
	Tag    string
	Server string
	Port   int
	Raw    map[string]any
}

func Import(input []byte, options Options) ([]Outbound, error) {
	if len(input) == 0 {
		return nil, errors.New("import input is empty")
	}
	if len(input) > MaxImportBytes {
		return nil, fmt.Errorf("import input exceeds %d bytes", MaxImportBytes)
	}
	payload := bytes.TrimSpace(input)
	if len(payload) == 0 {
		return nil, errors.New("import input is empty")
	}
	if payload[0] == '{' {
		outbound, err := parseJSONOutbound(payload, options)
		if err != nil {
			return nil, err
		}
		return []Outbound{outbound}, nil
	}

	text := string(payload)
	if !strings.Contains(text, "://") {
		decoded, err := decodeSubscription(text)
		if err != nil {
			return nil, errors.New("import input is neither a supported URI list, JSON, nor Base64 subscription")
		}
		if len(decoded) > MaxImportBytes {
			return nil, fmt.Errorf("decoded subscription exceeds %d bytes", MaxImportBytes)
		}
		text = string(decoded)
	}

	lines := nonEmptyLines(text)
	if len(lines) == 0 {
		return nil, errors.New("subscription contains no nodes")
	}
	if len(lines) > MaxNodes {
		return nil, fmt.Errorf("subscription contains more than %d nodes", MaxNodes)
	}
	outbounds := make([]Outbound, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		if _, duplicate := seen[line]; duplicate {
			continue
		}
		outbound, err := parseShareLink(line, options)
		if err != nil {
			return nil, fmt.Errorf("invalid outbound %d: %w", len(outbounds)+1, err)
		}
		seen[line] = struct{}{}
		outbound.Tag = fmt.Sprintf("remote-%d", len(outbounds)+1)
		outbound.Raw["tag"] = outbound.Tag
		outbounds = append(outbounds, outbound)
	}
	return outbounds, nil
}

func parseJSONOutbound(payload []byte, options Options) (Outbound, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return Outbound{}, fmt.Errorf("decode sing-box outbound JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Outbound{}, errors.New("sing-box outbound JSON must contain one object")
	}
	typeName, _ := raw["type"].(string)
	if err := validateType(typeName, options); err != nil {
		return Outbound{}, err
	}
	server, _ := raw["server"].(string)
	if strings.TrimSpace(server) == "" {
		return Outbound{}, errors.New("sing-box outbound server is required")
	}
	port, err := jsonPort(raw["server_port"])
	if err != nil {
		return Outbound{}, err
	}
	tag, _ := raw["tag"].(string)
	if tag == "" {
		tag = "remote-1"
		raw["tag"] = tag
	}
	return Outbound{Type: typeName, Tag: tag, Server: server, Port: port, Raw: raw}, nil
}

func parseShareLink(link string, options Options) (Outbound, error) {
	if strings.HasPrefix(strings.ToLower(link), "vmess://") {
		return parseVMessLink(link)
	}
	parsed, err := url.Parse(link)
	if err != nil {
		return Outbound{}, fmt.Errorf("parse URI: %w", err)
	}
	typeName := normalizeScheme(parsed.Scheme)
	if err := validateType(typeName, options); err != nil {
		return Outbound{}, err
	}
	server := parsed.Hostname()
	if server == "" {
		return Outbound{}, errors.New("proxy server is required")
	}
	portText := parsed.Port()
	if portText == "" {
		return Outbound{}, errors.New("proxy server port is required")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return Outbound{}, errors.New("proxy server port is invalid")
	}
	if err := validateURIIdentity(typeName, parsed); err != nil {
		return Outbound{}, err
	}
	raw, err := canonicalURIOutbound(typeName, parsed, server, port)
	if err != nil {
		return Outbound{}, err
	}
	return Outbound{
		Type:   typeName,
		Server: server,
		Port:   port,
		Raw:    raw,
	}, nil
}

func parseVMessLink(link string) (Outbound, error) {
	encoded := strings.TrimPrefix(link, "vmess://")
	payload, err := decodeBase64(encoded)
	if err != nil {
		return Outbound{}, errors.New("VMess URI payload is not valid Base64")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return Outbound{}, errors.New("VMess URI payload is not valid JSON")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Outbound{}, errors.New("VMess URI payload must contain one JSON object")
	}
	server, _ := raw["add"].(string)
	if server == "" {
		return Outbound{}, errors.New("VMess server is required")
	}
	port, err := jsonPort(raw["port"])
	if err != nil {
		return Outbound{}, fmt.Errorf("VMess %w", err)
	}
	uuid, _ := raw["id"].(string)
	if uuid == "" {
		return Outbound{}, errors.New("VMess UUID is required")
	}
	canonical := baseOutbound("vmess", server, port)
	canonical["uuid"] = uuid
	if security, _ := raw["scy"].(string); security != "" {
		canonical["security"] = security
	}
	if tlsMode, _ := raw["tls"].(string); tlsMode == "tls" {
		serverName, _ := raw["sni"].(string)
		canonical["tls"] = outboundTLS(serverName, server)
	}
	transportType, _ := raw["net"].(string)
	switch transportType {
	case "", "tcp":
	case "ws":
		path, _ := raw["path"].(string)
		if path == "" {
			return Outbound{}, errors.New("VMess WebSocket path is required")
		}
		canonical["transport"] = map[string]any{"type": "ws", "path": path}
	case "grpc":
		serviceName, _ := raw["path"].(string)
		if serviceName == "" {
			return Outbound{}, errors.New("VMess gRPC service name is required")
		}
		canonical["transport"] = map[string]any{"type": "grpc", "service_name": serviceName}
	default:
		return Outbound{}, fmt.Errorf("unsupported VMess transport %q", transportType)
	}
	return Outbound{Type: "vmess", Server: server, Port: port, Raw: canonical}, nil
}

func canonicalURIOutbound(typeName string, parsed *url.URL, server string, port int) (map[string]any, error) {
	raw := baseOutbound(typeName, server, port)
	username := parsed.User.Username()
	password, hasPassword := parsed.User.Password()
	query := parsed.Query()

	switch typeName {
	case "vless":
		raw["uuid"] = username
		security := strings.ToLower(query.Get("security"))
		switch security {
		case "", "none":
		case "tls":
			raw["tls"] = outboundTLS(query.Get("sni"), server)
		default:
			return nil, fmt.Errorf("unsupported VLESS security %q", security)
		}
		if err := applyURITransport(raw, query); err != nil {
			return nil, err
		}
	case "hysteria2":
		raw["password"] = username
		if hasPassword {
			raw["password"] = username + ":" + password
		}
		raw["tls"] = outboundTLS(query.Get("sni"), server)
	case "tuic":
		raw["uuid"] = username
		raw["password"] = password
		raw["tls"] = outboundTLS(query.Get("sni"), server)
	case "anytls":
		raw["password"] = username
		raw["tls"] = outboundTLS(query.Get("sni"), server)
	case "shadowsocks":
		decoded, err := decodeBase64(username)
		if err != nil {
			return nil, errors.New("Shadowsocks SIP002 user info is invalid")
		}
		method, secret, found := strings.Cut(string(decoded), ":")
		if !found || method == "" || secret == "" {
			return nil, errors.New("Shadowsocks SIP002 user info is invalid")
		}
		raw["method"] = method
		raw["password"] = secret
	case "socks":
		raw["version"] = "5"
		raw["username"] = username
		raw["password"] = password
	case "http":
		raw["username"] = username
		raw["password"] = password
		if strings.EqualFold(parsed.Scheme, "https") {
			raw["tls"] = outboundTLS(query.Get("sni"), server)
		}
	default:
		return nil, fmt.Errorf("unsupported outbound type %q", typeName)
	}
	return raw, nil
}

func baseOutbound(typeName, server string, port int) map[string]any {
	return map[string]any{
		"type":        typeName,
		"server":      server,
		"server_port": port,
	}
}

func outboundTLS(serverName, fallback string) map[string]any {
	if serverName == "" {
		serverName = fallback
	}
	return map[string]any{
		"enabled":     true,
		"server_name": serverName,
	}
}

func applyURITransport(raw map[string]any, query url.Values) error {
	transportType := strings.ToLower(query.Get("type"))
	switch transportType {
	case "", "tcp":
		return nil
	case "ws", "websocket":
		path := query.Get("path")
		if path == "" {
			return errors.New("WebSocket path is required")
		}
		raw["transport"] = map[string]any{"type": "ws", "path": path}
		return nil
	case "grpc":
		serviceName := query.Get("serviceName")
		if serviceName == "" {
			serviceName = query.Get("service_name")
		}
		if serviceName == "" {
			return errors.New("gRPC service name is required")
		}
		raw["transport"] = map[string]any{"type": "grpc", "service_name": serviceName}
		return nil
	default:
		return fmt.Errorf("unsupported transport %q", transportType)
	}
}

func validateURIIdentity(typeName string, parsed *url.URL) error {
	if parsed.User == nil || parsed.User.Username() == "" {
		return fmt.Errorf("%s credential is required", typeName)
	}
	switch typeName {
	case "tuic", "socks", "http":
		if _, exists := parsed.User.Password(); !exists {
			return fmt.Errorf("%s password is required", typeName)
		}
	case "shadowsocks":
		decoded, err := decodeBase64(parsed.User.Username())
		if err != nil || !bytes.Contains(decoded, []byte(":")) {
			return errors.New("Shadowsocks SIP002 user info is invalid")
		}
	}
	return nil
}

func validateType(typeName string, options Options) error {
	switch typeName {
	case "vless", "vmess", "hysteria2", "tuic", "anytls", "shadowsocks":
		return nil
	case "socks", "http":
		if options.AllowIPv4Proxy {
			return nil
		}
		return fmt.Errorf("%s outbound is only allowed for the IPv4 fallback mode", typeName)
	default:
		return fmt.Errorf("unsupported outbound type %q", typeName)
	}
}

func normalizeScheme(scheme string) string {
	switch strings.ToLower(scheme) {
	case "hy2", "hysteria2":
		return "hysteria2"
	case "socks", "socks5":
		return "socks"
	case "ss":
		return "shadowsocks"
	case "https", "http":
		return "http"
	default:
		return strings.ToLower(scheme)
	}
}

func jsonPort(value any) (int, error) {
	var text string
	switch typed := value.(type) {
	case json.Number:
		text = typed.String()
	case string:
		text = typed
	default:
		return 0, errors.New("server_port is required")
	}
	port, err := strconv.Atoi(text)
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New("server_port is invalid")
	}
	return port, nil
}

func decodeSubscription(encoded string) ([]byte, error) {
	compact := strings.Map(func(character rune) rune {
		if character == '\r' || character == '\n' || character == ' ' || character == '\t' {
			return -1
		}
		return character
	}, encoded)
	return decodeBase64(compact)
}

func decodeBase64(encoded string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		if decoded, err := encoding.DecodeString(encoded); err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("invalid Base64")
}

func nonEmptyLines(text string) []string {
	lines := make([]string, 0)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
