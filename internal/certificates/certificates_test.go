package certificates

import (
	"crypto/rand"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateSelfSignedCreatesMatchingCertificateAndFingerprint(t *testing.T) {
	now := time.Date(2026, 8, 2, 2, 0, 0, 0, time.UTC)
	material, err := GenerateSelfSigned(SelfSignedOptions{
		ServerNames: []string{"panel.example.test"},
		IPAddresses: []netip.Addr{netip.MustParseAddr("2001:db8::10")},
		Now:         now,
		Entropy:     rand.Reader,
	})
	if err != nil {
		t.Fatalf("GenerateSelfSigned: %v", err)
	}
	if len(material.CertificatePEM) == 0 || len(material.PrivateKeyPEM) == 0 {
		t.Fatal("generated certificate material is empty")
	}
	if !strings.HasPrefix(material.FingerprintSHA256, "SHA256:") {
		t.Fatalf("fingerprint = %q", material.FingerprintSHA256)
	}

	info, err := ValidatePEM(material.CertificatePEM, material.PrivateKeyPEM, now, "panel.example.test")
	if err != nil {
		t.Fatalf("ValidatePEM: %v", err)
	}
	if info.NotAfter.Before(now.Add(89 * 24 * time.Hour)) {
		t.Fatalf("NotAfter = %s, want at least 89 days", info.NotAfter)
	}
	if info.FingerprintSHA256 != material.FingerprintSHA256 {
		t.Fatalf("validated fingerprint = %q, generated = %q", info.FingerprintSHA256, material.FingerprintSHA256)
	}
}

func TestValidatePEMRejectsMismatchExpiryAndWrongServerName(t *testing.T) {
	now := time.Date(2026, 8, 2, 2, 0, 0, 0, time.UTC)
	first := mustSelfSigned(t, now, "first.example.test")
	second := mustSelfSigned(t, now, "second.example.test")

	tests := map[string]struct {
		certificate []byte
		key         []byte
		when        time.Time
		serverName  string
	}{
		"mismatched private key": {certificate: first.CertificatePEM, key: second.PrivateKeyPEM, when: now, serverName: "first.example.test"},
		"expired certificate":    {certificate: first.CertificatePEM, key: first.PrivateKeyPEM, when: now.Add(91 * 24 * time.Hour), serverName: "first.example.test"},
		"wrong server name":      {certificate: first.CertificatePEM, key: first.PrivateKeyPEM, when: now, serverName: "other.example.test"},
		"malformed certificate":  {certificate: []byte("not pem"), key: first.PrivateKeyPEM, when: now, serverName: "first.example.test"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidatePEM(test.certificate, test.key, test.when, test.serverName); err == nil {
				t.Fatal("ValidatePEM accepted invalid certificate material")
			}
		})
	}
}

func TestValidateExternalFilesRequiresRootOnlyPrivateKey(t *testing.T) {
	now := time.Date(2026, 8, 2, 2, 0, 0, 0, time.UTC)
	material := mustSelfSigned(t, now, "panel.example.test")
	directory := t.TempDir()
	certificatePath := filepath.Join(directory, "panel.crt")
	keyPath := filepath.Join(directory, "panel.key")
	if err := os.WriteFile(certificatePath, material.CertificatePEM, 0o644); err != nil {
		t.Fatalf("WriteFile certificate: %v", err)
	}
	if err := os.WriteFile(keyPath, material.PrivateKeyPEM, 0o600); err != nil {
		t.Fatalf("WriteFile key: %v", err)
	}

	if _, err := ValidateExternalFiles(certificatePath, keyPath, now, "panel.example.test"); err != nil {
		t.Fatalf("ValidateExternalFiles: %v", err)
	}
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatalf("Chmod key: %v", err)
	}
	if _, err := ValidateExternalFiles(certificatePath, keyPath, now, "panel.example.test"); err == nil {
		t.Fatal("ValidateExternalFiles accepted group/world-readable private key")
	}
	if _, err := ValidateExternalFiles(certificatePath, filepath.Join(directory, "missing.key"), now, "panel.example.test"); err == nil {
		t.Fatal("ValidateExternalFiles accepted missing private key")
	}
}

func TestGenerateSelfSignedRejectsUnsafeOptions(t *testing.T) {
	now := time.Date(2026, 8, 2, 2, 0, 0, 0, time.UTC)
	tests := map[string]SelfSignedOptions{
		"missing names": {Now: now, Entropy: rand.Reader},
		"nil entropy":   {ServerNames: []string{"panel.example.test"}, Now: now},
		"invalid name":  {ServerNames: []string{"bad/name"}, Now: now, Entropy: rand.Reader},
		"non global IP": {IPAddresses: []netip.Addr{netip.MustParseAddr("::1")}, Now: now, Entropy: rand.Reader},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := GenerateSelfSigned(options); err == nil {
				t.Fatal("GenerateSelfSigned accepted unsafe options")
			}
		})
	}
}

func mustSelfSigned(t *testing.T, now time.Time, serverName string) Material {
	t.Helper()
	material, err := GenerateSelfSigned(SelfSignedOptions{ServerNames: []string{serverName}, Now: now, Entropy: rand.Reader})
	if err != nil {
		t.Fatalf("GenerateSelfSigned: %v", err)
	}
	return material
}
