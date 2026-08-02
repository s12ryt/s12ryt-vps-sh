package domain

import (
	"bytes"
	"errors"
	"regexp"
	"testing"
)

func TestGenerateNodeCredentialCreatesProtocolSpecificSecrets(t *testing.T) {
	uuidPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	tests := []struct {
		protocol     Protocol
		wantUUID     bool
		wantUsername bool
		wantPassword bool
		wantMethod   string
	}{
		{protocol: ProtocolVLESS, wantUUID: true},
		{protocol: ProtocolVMess, wantUUID: true},
		{protocol: ProtocolHysteria2, wantPassword: true},
		{protocol: ProtocolTUIC, wantUUID: true, wantPassword: true},
		{protocol: ProtocolSOCKS5, wantUsername: true, wantPassword: true},
		{protocol: ProtocolAnyTLS, wantPassword: true},
		{protocol: ProtocolShadowsocks, wantPassword: true, wantMethod: "aes-256-gcm"},
	}

	for index, test := range tests {
		t.Run(string(test.protocol), func(t *testing.T) {
			entropy := bytes.NewReader(bytes.Repeat([]byte{byte(index + 1)}, 256))
			credential, err := GenerateNodeCredential(test.protocol, entropy)
			if err != nil {
				t.Fatalf("GenerateNodeCredential() error = %v", err)
			}
			if err := credential.Validate(test.protocol); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if test.wantUUID != uuidPattern.MatchString(credential.UUID) {
				t.Fatalf("UUID = %q, want present = %v", credential.UUID, test.wantUUID)
			}
			if test.wantUsername != (len(credential.Username) == 12) {
				t.Fatalf("username length = %d, want present = %v", len(credential.Username), test.wantUsername)
			}
			if test.wantPassword != (len(credential.Password) == 24) {
				t.Fatalf("password length = %d, want present = %v", len(credential.Password), test.wantPassword)
			}
			if credential.Method != test.wantMethod {
				t.Fatalf("method = %q, want %q", credential.Method, test.wantMethod)
			}
		})
	}
}

func TestGeneratedNodeCredentialsAreIndependent(t *testing.T) {
	entropy := bytes.NewReader([]byte{
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
		16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31,
	})
	first, err := GenerateNodeCredential(ProtocolVLESS, entropy)
	if err != nil {
		t.Fatalf("first GenerateNodeCredential() error = %v", err)
	}
	second, err := GenerateNodeCredential(ProtocolVLESS, entropy)
	if err != nil {
		t.Fatalf("second GenerateNodeCredential() error = %v", err)
	}
	if first.UUID == second.UUID {
		t.Fatal("separate nodes received the same UUID")
	}
}

func TestGenerateNodeCredentialRejectsUnsafeInputs(t *testing.T) {
	if _, err := GenerateNodeCredential(ProtocolVLESS, nil); !errors.Is(err, ErrInsufficientEntropy) {
		t.Fatalf("nil entropy error = %v", err)
	}
	if _, err := GenerateNodeCredential(Protocol("unknown"), bytes.NewReader(make([]byte, 64))); err == nil {
		t.Fatal("unknown protocol was accepted")
	}
}

func TestNodeCredentialValidationRejectsMissingOrUnexpectedFields(t *testing.T) {
	validUUID := "00112233-4455-4677-8899-aabbccddeeff"
	tests := []struct {
		name       string
		protocol   Protocol
		credential NodeCredential
	}{
		{name: "vless missing UUID", protocol: ProtocolVLESS},
		{name: "vless unexpected password", protocol: ProtocolVLESS, credential: NodeCredential{UUID: validUUID, Password: "unexpected"}},
		{name: "socks missing username", protocol: ProtocolSOCKS5, credential: NodeCredential{Password: "abcdefghijklmnopqrstuvwx"}},
		{name: "shadowsocks missing method", protocol: ProtocolShadowsocks, credential: NodeCredential{Password: "abcdefghijklmnopqrstuvwx"}},
		{name: "unknown protocol", protocol: Protocol("unknown"), credential: NodeCredential{Password: "abcdefghijklmnopqrstuvwx"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.credential.Validate(test.protocol); err == nil {
				t.Fatal("invalid credential was accepted")
			}
		})
	}
}

func TestConfigValidationRequiresNodeCredentials(t *testing.T) {
	config := DefaultConfig()
	config.Nodes = []Node{{
		ID:       "node-1",
		Protocol: ProtocolVLESS,
		Port:     24443,
		Enabled:  true,
	}}
	if err := config.Validate(); err == nil {
		t.Fatal("node without protocol credential was accepted")
	}

	config.Nodes[0].Credential = NodeCredential{UUID: "00112233-4455-4677-8899-aabbccddeeff"}
	if err := config.Validate(); err != nil {
		t.Fatalf("valid credential rejected: %v", err)
	}
}
