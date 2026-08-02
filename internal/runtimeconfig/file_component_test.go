package runtimeconfig

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
	"github.com/s12ryt/s12ryt-vps-sh/internal/singbox"
)

func TestFileComponentAppliesCandidateAndRollsBackToPreviousConfig(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sing-box.json")
	current := domain.DefaultConfig()
	candidate := configuredRuntimeConfig()
	currentPayload, err := CompileServerConfig(Input{Config: current})
	if err != nil {
		t.Fatalf("compile current fixture: %v", err)
	}
	if err := os.WriteFile(path, currentPayload, 0o600); err != nil {
		t.Fatalf("write current fixture: %v", err)
	}

	var validated []byte
	component, err := NewFileComponent(FileComponentOptions{
		Path: path,
		Resolve: func(config domain.Config) (Input, error) {
			return runtimeInputForConfig(config), nil
		},
		Validate: func(payload []byte) error {
			validated = append([]byte(nil), payload...)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewFileComponent() error = %v", err)
	}
	change, err := component.Prepare(context.Background(), current, candidate)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if len(validated) == 0 {
		t.Fatal("candidate configuration was not validated")
	}
	if err := change.Apply(context.Background()); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	applied, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read applied configuration: %v", err)
	}
	if string(applied) != string(validated) {
		t.Fatalf("applied config does not match validated candidate\n got: %s\nwant: %s", applied, validated)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat applied configuration: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("applied config mode = %04o, want 0600", info.Mode().Perm())
	}
	if err := change.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read restored configuration: %v", err)
	}
	if string(restored) != string(currentPayload) {
		t.Fatalf("restored config differs from previous compiled config\n got: %s\nwant: %s", restored, currentPayload)
	}
}

func TestFileComponentRejectsCandidateBeforeMutatingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sing-box.json")
	sentinel := []byte("old-config\n")
	if err := os.WriteFile(path, sentinel, 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	wantErr := errors.New("sing-box check failed")
	component, err := NewFileComponent(FileComponentOptions{
		Path: path,
		Resolve: func(config domain.Config) (Input, error) {
			return runtimeInputForConfig(config), nil
		},
		Validate: func([]byte) error { return wantErr },
	})
	if err != nil {
		t.Fatalf("NewFileComponent() error = %v", err)
	}
	if _, err := component.Prepare(context.Background(), domain.DefaultConfig(), configuredRuntimeConfig()); !errors.Is(err, wantErr) {
		t.Fatalf("Prepare() error = %v, want errors.Is(%v)", err, wantErr)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	if string(contents) != string(sentinel) {
		t.Fatalf("configuration changed after failed validation: %q", contents)
	}
}

func TestNewFileComponentRejectsUnsafeDependencies(t *testing.T) {
	valid := FileComponentOptions{
		Path:     "/opt/s12ryt-ipv6/config/sing-box.json",
		Resolve:  func(domain.Config) (Input, error) { return Input{}, nil },
		Validate: func([]byte) error { return nil },
	}
	for name, mutate := range map[string]func(*FileComponentOptions){
		"relative path":     func(options *FileComponentOptions) { options.Path = "sing-box.json" },
		"missing resolver":  func(options *FileComponentOptions) { options.Resolve = nil },
		"missing validator": func(options *FileComponentOptions) { options.Validate = nil },
	} {
		t.Run(name, func(t *testing.T) {
			options := valid
			mutate(&options)
			if _, err := NewFileComponent(options); err == nil {
				t.Fatal("NewFileComponent() error = nil, want rejection")
			}
		})
	}
}

func configuredRuntimeConfig() domain.Config {
	config := domain.DefaultConfig()
	config.Nodes = []domain.Node{{
		ID:         "edge",
		Protocol:   domain.ProtocolVLESS,
		Port:       24443,
		Enabled:    true,
		Credential: domain.NodeCredential{UUID: "123e4567-e89b-42d3-a456-426614174000"},
	}}
	return config
}

func runtimeInputForConfig(config domain.Config) Input {
	input := Input{Config: config}
	if len(config.Nodes) == 0 {
		return input
	}
	input.Deployments = []NodeDeployment{{
		NodeID:    "edge",
		Listeners: []netip.Addr{netip.MustParseAddr("2001:db8::7")},
		TLS: singbox.TLSConfig{
			Enabled:         true,
			CertificatePath: "/opt/s12ryt-ipv6/tls/server.crt",
			KeyPath:         "/opt/s12ryt-ipv6/tls/server.key",
		},
	}}
	input.IPv6Outbounds = []netip.Addr{netip.MustParseAddr("2001:db8:1::10")}
	return input
}
