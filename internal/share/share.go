package share

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

var safeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

type Input struct {
	LocalNodes    []LocalNode
	RemoteSecrets []string
}

type LocalNode struct {
	ID        string
	Name      string
	Protocol  domain.Protocol
	Server    netip.Addr
	Port      int
	Username  string
	UUID      string
	Password  string
	Method    string
	Enabled   bool
	Healthy   bool
	TLS       TLSOptions
	Transport TransportOptions
}

type TLSOptions struct {
	ServerName string
}

type TransportOptions struct {
	Type        string
	Path        string
	ServiceName string
}

type Artifact struct {
	NodeID     string
	URI        string
	QRPayload  string
	ClientJSON []byte
}

type Bundle struct {
	Nodes        []Artifact
	Subscription string
}

func GenerateBundle(input Input) (Bundle, error) {
	bundle := Bundle{Nodes: make([]Artifact, 0, len(input.LocalNodes))}
	subscription := make([]string, 0, len(input.LocalNodes))
	seen := make(map[string]struct{}, len(input.LocalNodes))
	for _, node := range input.LocalNodes {
		if err := validateLocalNode(node); err != nil {
			return Bundle{}, fmt.Errorf("node %q: %w", node.ID, err)
		}
		if _, duplicate := seen[node.ID]; duplicate {
			return Bundle{}, fmt.Errorf("duplicate node ID %q", node.ID)
		}
		seen[node.ID] = struct{}{}

		uri, err := nodeURI(node)
		if err != nil {
			return Bundle{}, fmt.Errorf("node %q URI: %w", node.ID, err)
		}
		clientJSON, err := clientConfig(node)
		if err != nil {
			return Bundle{}, fmt.Errorf("node %q client JSON: %w", node.ID, err)
		}
		artifact := Artifact{NodeID: node.ID, URI: uri, QRPayload: uri, ClientJSON: clientJSON}
		bundle.Nodes = append(bundle.Nodes, artifact)
		if node.Enabled && node.Healthy {
			subscription = append(subscription, uri)
		}
	}
	bundle.Subscription = base64.StdEncoding.EncodeToString([]byte(strings.Join(subscription, "\n")))
	return bundle, nil
}

func validateLocalNode(node LocalNode) error {
	if !safeIDPattern.MatchString(node.ID) {
		return errors.New("invalid node ID")
	}
	if !node.Server.IsValid() || !node.Server.IsGlobalUnicast() {
		return errors.New("server must be a global unicast address")
	}
	if node.Port < 20000 || node.Port > 49999 {
		return errors.New("port is outside 20000-49999")
	}
	switch node.Protocol {
	case domain.ProtocolVLESS, domain.ProtocolVMess:
		if node.UUID == "" {
			return errors.New("UUID is required")
		}
	case domain.ProtocolTUIC:
		if node.UUID == "" || node.Password == "" {
			return errors.New("TUIC UUID and password are required")
		}
	case domain.ProtocolHysteria2, domain.ProtocolAnyTLS:
		if node.Password == "" {
			return errors.New("password is required")
		}
	case domain.ProtocolSOCKS5:
		if node.Username == "" || node.Password == "" {
			return errors.New("SOCKS5 username and password are required")
		}
	case domain.ProtocolShadowsocks:
		if node.Method == "" || node.Password == "" {
			return errors.New("Shadowsocks method and password are required")
		}
	default:
		return fmt.Errorf("unsupported protocol %q", node.Protocol)
	}
	if node.Transport.Type != "" && node.Transport.Type != "ws" && node.Transport.Type != "grpc" {
		return fmt.Errorf("unsupported transport %q", node.Transport.Type)
	}
	return nil
}

func nodeURI(node LocalNode) (string, error) {
	host := net.JoinHostPort(node.Server.String(), strconv.Itoa(node.Port))
	name := url.QueryEscape(node.Name)
	query := url.Values{}
	if node.TLS.ServerName != "" {
		query.Set("security", "tls")
		query.Set("sni", node.TLS.ServerName)
	}
	if node.Transport.Type != "" {
		query.Set("type", node.Transport.Type)
		if node.Transport.Type == "ws" {
			query.Set("path", node.Transport.Path)
		} else {
			query.Set("serviceName", node.Transport.ServiceName)
		}
	}

	suffix := ""
	if encoded := query.Encode(); encoded != "" {
		suffix = "?" + encoded
	}
	suffix += "#" + name

	switch node.Protocol {
	case domain.ProtocolVLESS:
		return "vless://" + url.QueryEscape(node.UUID) + "@" + host + suffix, nil
	case domain.ProtocolVMess:
		payload := map[string]string{
			"v": "2", "ps": node.Name, "add": node.Server.String(), "port": strconv.Itoa(node.Port),
			"id": node.UUID, "aid": "0", "scy": "auto", "net": transportOrTCP(node.Transport.Type),
			"type": "none", "host": node.TLS.ServerName, "path": node.Transport.Path,
		}
		if node.TLS.ServerName != "" {
			payload["tls"] = "tls"
			payload["sni"] = node.TLS.ServerName
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		return "vmess://" + base64.StdEncoding.EncodeToString(encoded), nil
	case domain.ProtocolHysteria2:
		return "hysteria2://" + url.QueryEscape(node.Password) + "@" + host + suffix, nil
	case domain.ProtocolTUIC:
		return "tuic://" + url.QueryEscape(node.UUID) + ":" + url.QueryEscape(node.Password) + "@" + host + suffix, nil
	case domain.ProtocolSOCKS5:
		return "socks5://" + url.QueryEscape(node.Username) + ":" + url.QueryEscape(node.Password) + "@" + host + "#" + name, nil
	case domain.ProtocolAnyTLS:
		return "anytls://" + url.QueryEscape(node.Password) + "@" + host + suffix, nil
	case domain.ProtocolShadowsocks:
		credential := base64.RawURLEncoding.EncodeToString([]byte(node.Method + ":" + node.Password))
		return "ss://" + credential + "@" + host + "#" + name, nil
	default:
		return "", fmt.Errorf("unsupported protocol %q", node.Protocol)
	}
}

func clientConfig(node LocalNode) ([]byte, error) {
	outbound := map[string]any{
		"type":        outboundType(node.Protocol),
		"tag":         "proxy",
		"server":      node.Server.String(),
		"server_port": node.Port,
	}
	switch node.Protocol {
	case domain.ProtocolVLESS, domain.ProtocolVMess:
		outbound["uuid"] = node.UUID
	case domain.ProtocolTUIC:
		outbound["uuid"] = node.UUID
		outbound["password"] = node.Password
	case domain.ProtocolHysteria2, domain.ProtocolAnyTLS:
		outbound["password"] = node.Password
	case domain.ProtocolSOCKS5:
		outbound["username"] = node.Username
		outbound["password"] = node.Password
	case domain.ProtocolShadowsocks:
		outbound["method"] = node.Method
		outbound["password"] = node.Password
	default:
		return nil, fmt.Errorf("unsupported protocol %q", node.Protocol)
	}
	if node.TLS.ServerName != "" {
		outbound["tls"] = map[string]any{"enabled": true, "server_name": node.TLS.ServerName}
	}
	if node.Transport.Type != "" {
		transport := map[string]any{"type": node.Transport.Type}
		if node.Transport.Type == "ws" {
			transport["path"] = node.Transport.Path
		} else {
			transport["service_name"] = node.Transport.ServiceName
		}
		outbound["transport"] = transport
	}
	config := map[string]any{
		"log":       map[string]any{"level": "warn"},
		"outbounds": []map[string]any{outbound},
	}
	return json.MarshalIndent(config, "", "  ")
}

func outboundType(protocol domain.Protocol) string {
	if protocol == domain.ProtocolSOCKS5 {
		return "socks"
	}
	return string(protocol)
}

func transportOrTCP(transport string) string {
	if transport == "" {
		return "tcp"
	}
	return transport
}
