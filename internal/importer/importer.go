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
	return Outbound{
		Type:   typeName,
		Server: server,
		Port:   port,
		Raw: map[string]any{
			"uri": link,
		},
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
	return Outbound{Type: "vmess", Server: server, Port: port, Raw: map[string]any{"uri": link}}, nil
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
