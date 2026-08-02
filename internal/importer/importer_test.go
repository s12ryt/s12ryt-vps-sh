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
