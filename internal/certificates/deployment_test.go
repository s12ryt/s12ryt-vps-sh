package certificates

import (
	"crypto/rand"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildDeploymentPlanCopiesValidatedExternalMaterial(t *testing.T) {
	now := time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC)
	material := mustSelfSigned(t, now, "panel.example.test")
	directory := t.TempDir()
	certificatePath := filepath.Join(directory, "external.crt")
	privateKeyPath := filepath.Join(directory, "external.key")
	writeCertificateFiles(t, certificatePath, privateKeyPath, material)

	plan, err := BuildDeploymentPlan(DeploymentOptions{
		Mode:                DeploymentExternalCopy,
		CertificatePath:     certificatePath,
		PrivateKeyPath:      privateKeyPath,
		ServerName:          "panel.example.test",
		Now:                 now,
		HTTPPort80Available: true,
	})
	if err != nil {
		t.Fatalf("BuildDeploymentPlan: %v", err)
	}
	if plan.CertificatePath != ProjectTLSDirectory+"/server.crt" || plan.PrivateKeyPath != ProjectTLSDirectory+"/server.key" {
		t.Fatalf("deployment paths = %q, %q", plan.CertificatePath, plan.PrivateKeyPath)
	}
	if plan.Files[plan.CertificatePath].Mode != 0o644 || plan.Files[plan.PrivateKeyPath].Mode != 0o600 {
		t.Fatalf("deployment modes = %#o, %#o", plan.Files[plan.CertificatePath].Mode, plan.Files[plan.PrivateKeyPath].Mode)
	}
	if string(plan.Files[plan.CertificatePath].Content) != string(material.CertificatePEM) ||
		string(plan.Files[plan.PrivateKeyPath].Content) != string(material.PrivateKeyPEM) {
		t.Fatal("deployment plan did not preserve validated certificate material")
	}
	if plan.FingerprintSHA256 != material.FingerprintSHA256 {
		t.Fatalf("fingerprint = %q", plan.FingerprintSHA256)
	}
}

func TestBuildDeploymentPlanReferencesValidatedExternalMaterial(t *testing.T) {
	now := time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC)
	material := mustSelfSigned(t, now, "panel.example.test")
	directory := t.TempDir()
	certificatePath := filepath.Join(directory, "external.crt")
	privateKeyPath := filepath.Join(directory, "external.key")
	writeCertificateFiles(t, certificatePath, privateKeyPath, material)

	plan, err := BuildDeploymentPlan(DeploymentOptions{
		Mode:                DeploymentExternalReference,
		CertificatePath:     certificatePath,
		PrivateKeyPath:      privateKeyPath,
		ServerName:          "panel.example.test",
		Now:                 now,
		HTTPPort80Available: true,
	})
	if err != nil {
		t.Fatalf("BuildDeploymentPlan: %v", err)
	}
	if plan.CertificatePath != certificatePath || plan.PrivateKeyPath != privateKeyPath {
		t.Fatalf("reference paths = %q, %q", plan.CertificatePath, plan.PrivateKeyPath)
	}
	if len(plan.Files) != 0 {
		t.Fatalf("reference plan unexpectedly copied %d files", len(plan.Files))
	}
}

func TestBuildDeploymentPlanDefinesSafeSelfSignedAndACMEPolicies(t *testing.T) {
	now := time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC)
	selfSigned, err := BuildDeploymentPlan(DeploymentOptions{
		Mode:                DeploymentSelfSigned,
		ServerName:          "panel.example.test",
		IPAddresses:         []netip.Addr{netip.MustParseAddr("2001:db8::20")},
		Now:                 now,
		Entropy:             rand.Reader,
		HTTPPort80Available: true,
	})
	if err != nil {
		t.Fatalf("BuildDeploymentPlan self-signed: %v", err)
	}
	if len(selfSigned.Files) != 2 || selfSigned.FingerprintSHA256 == "" {
		t.Fatalf("self-signed plan = %#v", selfSigned)
	}

	acme, err := BuildDeploymentPlan(DeploymentOptions{
		Mode:                DeploymentACMEHTTP01,
		ServerName:          "panel.example.test",
		Email:               "admin@example.test",
		Now:                 now,
		HTTPPort80Available: true,
	})
	if err != nil {
		t.Fatalf("BuildDeploymentPlan ACME: %v", err)
	}
	if acme.ChallengeAddress != ":80" || acme.RenewBefore != 30*24*time.Hour {
		t.Fatalf("ACME policy = %#v", acme)
	}
	if acme.RenewalFailurePolicy != RenewalPreserveExisting {
		t.Fatalf("renewal failure policy = %q", acme.RenewalFailurePolicy)
	}
	if acme.CertificatePath != ProjectTLSDirectory+"/acme.crt" || acme.PrivateKeyPath != ProjectTLSDirectory+"/acme.key" {
		t.Fatalf("ACME paths = %q, %q", acme.CertificatePath, acme.PrivateKeyPath)
	}
}

func TestBuildDeploymentPlanRejectsUnsafeInputs(t *testing.T) {
	now := time.Date(2026, 8, 2, 3, 0, 0, 0, time.UTC)
	tests := map[string]DeploymentOptions{
		"unknown mode":         {Mode: "manual", Now: now},
		"custom project root":  {Mode: DeploymentSelfSigned, ProjectRoot: "/tmp/project", ServerName: "panel.example.test", Now: now, Entropy: rand.Reader},
		"ACME missing domain":  {Mode: DeploymentACMEHTTP01, Now: now, HTTPPort80Available: true},
		"ACME port occupied":   {Mode: DeploymentACMEHTTP01, ServerName: "panel.example.test", Now: now},
		"invalid ACME email":   {Mode: DeploymentACMEHTTP01, ServerName: "panel.example.test", Email: "not-an-email", Now: now, HTTPPort80Available: true},
		"external missing key": {Mode: DeploymentExternalReference, CertificatePath: "/missing.crt", PrivateKeyPath: "/missing.key", ServerName: "panel.example.test", Now: now},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildDeploymentPlan(options); err == nil {
				t.Fatal("BuildDeploymentPlan accepted unsafe input")
			}
		})
	}
}

func writeCertificateFiles(t *testing.T, certificatePath string, privateKeyPath string, material Material) {
	t.Helper()
	if err := os.WriteFile(certificatePath, material.CertificatePEM, 0o644); err != nil {
		t.Fatalf("WriteFile certificate: %v", err)
	}
	if err := os.WriteFile(privateKeyPath, material.PrivateKeyPEM, 0o600); err != nil {
		t.Fatalf("WriteFile private key: %v", err)
	}
}
