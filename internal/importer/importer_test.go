package importer

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func TestImportShareLinksValidatesAndDeduplicatesNodes(t *testing.T) {
	input := strings.Join([]string{
		"vless://550e8400-e29b-41d4-a716-446655440000@example.com:443?security=tls#primary",
		"hysteria2://secret@[2001:db8::20]:8443?sni=example.com#hy2",
		"vless://550e8400-e29b-41d4-a716-446655440000@example.com:443?security=tls#primary",
	}, "\n")

	outbounds, err := Import([]byte(input), Options{})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if len(outbounds) != 2 {
		t.Fatalf("outbound count = %d, want 2", len(outbounds))
	}
	if outbounds[0].Type != "vless" || outbounds[0].Server != "example.com" || outbounds[0].Port != 443 {
		t.Fatalf("VLESS outbound = %#v", outbounds[0])
	}
	if outbounds[1].Type != "hysteria2" || outbounds[1].Server != "2001:db8::20" || outbounds[1].Port != 8443 {
		t.Fatalf("Hysteria2 outbound = %#v", outbounds[1])
	}
}

func TestImportBase64SubscriptionSupportsRequiredSchemes(t *testing.T) {
	links := strings.Join([]string{
		"tuic://550e8400-e29b-41d4-a716-446655440000:secret@example.com:443",
		"anytls://secret@example.net:8443",
		"socks5://user:secret@192.0.2.10:1080",
	}, "\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(links))

	outbounds, err := Import([]byte(encoded), Options{AllowIPv4Proxy: true})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	wantTypes := []string{"tuic", "anytls", "socks"}
	for index, want := range wantTypes {
		if outbounds[index].Type != want {
			t.Fatalf("outbound %d type = %q, want %q", index, outbounds[index].Type, want)
		}
	}
}

func TestImportSupportsVMessJSONLinkAndShadowsocksSIP002(t *testing.T) {
	vmessJSON := `{"v":"2","ps":"vmess-node","add":"vm.example.com","port":"443","id":"550e8400-e29b-41d4-a716-446655440000","net":"ws","tls":"tls"}`
	vmess := "vmess://" + base64.RawStdEncoding.EncodeToString([]byte(vmessJSON))
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:secret"))
	shadowsocks := "ss://" + userinfo + "@ss.example.com:8388#ss-node"

	outbounds, err := Import([]byte(vmess+"\n"+shadowsocks), Options{})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if outbounds[0].Type != "vmess" || outbounds[0].Server != "vm.example.com" || outbounds[0].Port != 443 {
		t.Fatalf("VMess outbound = %#v", outbounds[0])
	}
	if outbounds[1].Type != "shadowsocks" || outbounds[1].Server != "ss.example.com" || outbounds[1].Port != 8388 {
		t.Fatalf("Shadowsocks outbound = %#v", outbounds[1])
	}
}

func TestImportSingleSingBoxOutboundJSON(t *testing.T) {
	input := `{"type":"vless","tag":"remote-main","server":"proxy.example.com","server_port":443,"uuid":"550e8400-e29b-41d4-a716-446655440000"}`
	outbounds, err := Import([]byte(input), Options{})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if len(outbounds) != 1 || outbounds[0].Tag != "remote-main" || outbounds[0].Raw["uuid"] == nil {
		t.Fatalf("JSON outbound = %#v", outbounds)
	}
}

func TestImportRejectsUnsafeOrUnsupportedInput(t *testing.T) {
	overLimit := make([]byte, MaxImportBytes+1)
	tooMany := make([]string, MaxNodes+1)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("vless://550e8400-e29b-41d4-a716-44665544%04d@example.com:%d", index, 20000+index)
	}
	tests := []struct {
		name    string
		input   []byte
		options Options
	}{
		{name: "input exceeds one MiB", input: overLimit},
		{name: "more than one thousand nodes", input: []byte(strings.Join(tooMany, "\n"))},
		{name: "subscription URL is not fetched", input: []byte("https://subscriptions.example/list")},
		{name: "IPv4 proxy disabled", input: []byte("socks5://user:secret@192.0.2.10:1080")},
		{name: "unsupported scheme", input: []byte("trojan://secret@example.com:443")},
		{name: "missing server port", input: []byte("vless://550e8400-e29b-41d4-a716-446655440000@example.com")},
		{name: "JSON missing server", input: []byte(`{"type":"vless","server_port":443}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Import(test.input, test.options); err == nil {
				t.Fatal("Import() accepted unsafe input")
			}
		})
	}
}

func TestImportAllowsAuthenticatedHTTPProxyOnlyWhenEnabled(t *testing.T) {
	outbounds, err := Import([]byte("https://user:secret@proxy.example.com:8443"), Options{AllowIPv4Proxy: true})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if len(outbounds) != 1 || outbounds[0].Type != "http" || outbounds[0].Server != "proxy.example.com" {
		t.Fatalf("HTTP outbound = %#v", outbounds)
	}
}

func TestImportBuildsCanonicalSingBoxOutboundsFromShareLinks(t *testing.T) {
	vmessJSON := `{"v":"2","ps":"vmess-node","add":"vm.example.com","port":"443","id":"550e8400-e29b-41d4-a716-446655440000","net":"ws","path":"/vmess","sni":"vm.example.com","tls":"tls"}`
	shadowsocksUserInfo := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:shadow-secret"))
	input := strings.Join([]string{
		"vless://550e8400-e29b-41d4-a716-446655440000@vless.example.com:443?security=tls&sni=vless.example.com&type=ws&path=%2Fvless#vless",
		"vmess://" + base64.RawStdEncoding.EncodeToString([]byte(vmessJSON)),
		"hysteria2://hy2-secret@hy2.example.com:8443?sni=hy2.example.com#hy2",
		"tuic://550e8400-e29b-41d4-a716-446655440001:tuic-secret@tuic.example.com:443?sni=tuic.example.com#tuic",
		"anytls://any-secret@any.example.com:9443?sni=any.example.com#anytls",
		"ss://" + shadowsocksUserInfo + "@ss.example.com:8388#shadowsocks",
		"socks5://socks-user:socks-secret@192.0.2.10:1080#socks",
		"https://http-user:http-secret@proxy.example.com:8443#http",
	}, "\n")

	outbounds, err := Import([]byte(input), Options{AllowIPv4Proxy: true})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if len(outbounds) != 8 {
		t.Fatalf("outbound count = %d, want 8", len(outbounds))
	}
	for _, outbound := range outbounds {
		if outbound.Raw["type"] != outbound.Type || outbound.Raw["tag"] != outbound.Tag {
			t.Fatalf("outbound %q raw identity = %#v", outbound.Tag, outbound.Raw)
		}
		if outbound.Raw["server"] != outbound.Server || outbound.Raw["server_port"] != outbound.Port {
			t.Fatalf("outbound %q raw endpoint = %#v", outbound.Tag, outbound.Raw)
		}
		if _, exists := outbound.Raw["uri"]; exists {
			t.Fatalf("outbound %q retained non-runtime URI: %#v", outbound.Tag, outbound.Raw)
		}
	}

	assertRawString(t, outbounds[0].Raw, "uuid", "550e8400-e29b-41d4-a716-446655440000")
	assertRawTLS(t, outbounds[0].Raw, "vless.example.com")
	assertRawTransport(t, outbounds[0].Raw, "ws", "path", "/vless")
	assertRawString(t, outbounds[1].Raw, "uuid", "550e8400-e29b-41d4-a716-446655440000")
	assertRawTLS(t, outbounds[1].Raw, "vm.example.com")
	assertRawTransport(t, outbounds[1].Raw, "ws", "path", "/vmess")
	assertRawString(t, outbounds[2].Raw, "password", "hy2-secret")
	assertRawTLS(t, outbounds[2].Raw, "hy2.example.com")
	assertRawString(t, outbounds[3].Raw, "uuid", "550e8400-e29b-41d4-a716-446655440001")
	assertRawString(t, outbounds[3].Raw, "password", "tuic-secret")
	assertRawTLS(t, outbounds[3].Raw, "tuic.example.com")
	assertRawString(t, outbounds[4].Raw, "password", "any-secret")
	assertRawTLS(t, outbounds[4].Raw, "any.example.com")
	assertRawString(t, outbounds[5].Raw, "method", "aes-256-gcm")
	assertRawString(t, outbounds[5].Raw, "password", "shadow-secret")
	assertRawString(t, outbounds[6].Raw, "version", "5")
	assertRawString(t, outbounds[6].Raw, "username", "socks-user")
	assertRawString(t, outbounds[6].Raw, "password", "socks-secret")
	assertRawString(t, outbounds[7].Raw, "username", "http-user")
	assertRawString(t, outbounds[7].Raw, "password", "http-secret")
	assertRawTLS(t, outbounds[7].Raw, "proxy.example.com")
}

func assertRawString(t *testing.T, raw map[string]any, field, want string) {
	t.Helper()
	if got := raw[field]; got != want {
		t.Fatalf("raw field %q = %#v, want %q in %#v", field, got, want, raw)
	}
}

func assertRawTLS(t *testing.T, raw map[string]any, serverName string) {
	t.Helper()
	tlsConfig, ok := raw["tls"].(map[string]any)
	if !ok || tlsConfig["enabled"] != true || tlsConfig["server_name"] != serverName {
		t.Fatalf("raw TLS = %#v, want enabled with server_name %q in %#v", raw["tls"], serverName, raw)
	}
}

func assertRawTransport(t *testing.T, raw map[string]any, transportType, field, value string) {
	t.Helper()
	transport, ok := raw["transport"].(map[string]any)
	if !ok || transport["type"] != transportType || transport[field] != value {
		t.Fatalf("raw transport = %#v, want %s %s=%q in %#v", raw["transport"], transportType, field, value, raw)
	}
}
