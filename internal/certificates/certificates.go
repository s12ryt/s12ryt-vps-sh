package certificates

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"
)

const certificateLifetime = 90 * 24 * time.Hour
const maxCertificateFileSize = 1 << 20

type SelfSignedOptions struct {
	ServerNames []string
	IPAddresses []netip.Addr
	Now         time.Time
	Entropy     io.Reader
}

type Material struct {
	CertificatePEM   []byte
	PrivateKeyPEM    []byte
	FingerprintSHA256 string
	NotAfter          time.Time
}

type Info struct {
	FingerprintSHA256 string
	NotAfter          time.Time
}

func GenerateSelfSigned(options SelfSignedOptions) (Material, error) {
	if options.Entropy == nil {
		return Material{}, errors.New("certificate entropy is required")
	}
	if options.Now.IsZero() {
		return Material{}, errors.New("certificate time is required")
	}
	if len(options.ServerNames) == 0 && len(options.IPAddresses) == 0 {
		return Material{}, errors.New("at least one certificate name or IP address is required")
	}
	for _, name := range options.ServerNames {
		if !validServerName(name) {
			return Material{}, fmt.Errorf("invalid certificate server name %q", name)
		}
	}
	ipAddresses := make([]net.IP, 0, len(options.IPAddresses))
	for _, address := range options.IPAddresses {
		if !address.IsValid() || !address.IsGlobalUnicast() {
			return Material{}, fmt.Errorf("certificate IP %q must be global unicast", address)
		}
		ipAddresses = append(ipAddresses, net.IP(address.AsSlice()))
	}

	publicKey, privateKey, err := ed25519.GenerateKey(options.Entropy)
	if err != nil {
		return Material{}, fmt.Errorf("generate certificate key: %w", err)
	}
	serial, err := randomSerial(options.Entropy)
	if err != nil {
		return Material{}, err
	}
	commonName := "s12ryt-ipv6"
	if len(options.ServerNames) > 0 {
		commonName = options.ServerNames[0]
	} else if len(options.IPAddresses) > 0 {
		commonName = options.IPAddresses[0].String()
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             options.Now.Add(-5 * time.Minute),
		NotAfter:              options.Now.Add(certificateLifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              append([]string(nil), options.ServerNames...),
		IPAddresses:           ipAddresses,
	}
	certificateDER, err := x509.CreateCertificate(options.Entropy, template, template, publicKey, privateKey)
	if err != nil {
		return Material{}, fmt.Errorf("create self-signed certificate: %w", err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return Material{}, fmt.Errorf("marshal certificate key: %w", err)
	}

	return Material{
		CertificatePEM:   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}),
		PrivateKeyPEM:    pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER}),
		FingerprintSHA256: fingerprint(certificateDER),
		NotAfter:          template.NotAfter,
	}, nil
}

func ValidatePEM(certificatePEM []byte, privateKeyPEM []byte, now time.Time, serverName string) (Info, error) {
	if now.IsZero() {
		return Info{}, errors.New("certificate validation time is required")
	}
	if _, err := tls.X509KeyPair(certificatePEM, privateKeyPEM); err != nil {
		return Info{}, fmt.Errorf("certificate and private key do not match: %w", err)
	}
	block, _ := pem.Decode(certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return Info{}, errors.New("certificate PEM is invalid")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return Info{}, fmt.Errorf("parse certificate: %w", err)
	}
	if now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
		return Info{}, errors.New("certificate is not currently valid")
	}
	if serverName != "" {
		if err := certificate.VerifyHostname(serverName); err != nil {
			return Info{}, fmt.Errorf("certificate server name validation failed: %w", err)
		}
	}
	return Info{FingerprintSHA256: fingerprint(certificate.Raw), NotAfter: certificate.NotAfter}, nil
}

func ValidateExternalFiles(certificatePath string, privateKeyPath string, now time.Time, serverName string) (Info, error) {
	certificatePEM, err := readRegularFile(certificatePath, false)
	if err != nil {
		return Info{}, fmt.Errorf("read certificate: %w", err)
	}
	privateKeyPEM, err := readRegularFile(privateKeyPath, true)
	if err != nil {
		return Info{}, fmt.Errorf("read private key: %w", err)
	}
	return ValidatePEM(certificatePEM, privateKeyPEM, now, serverName)
}

func readRegularFile(path string, private bool) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}
	if info.Size() <= 0 || info.Size() > maxCertificateFileSize {
		return nil, errors.New("file size is outside the accepted range")
	}
	if private && (info.Mode().Perm()&0o077 != 0 || info.Mode().Perm()&0o400 == 0) {
		return nil, errors.New("private key must be owner-readable and inaccessible to group and others")
	}
	return os.ReadFile(path)
}

func randomSerial(reader io.Reader) (*big.Int, error) {
	serialBytes := make([]byte, 16)
	if _, err := io.ReadFull(reader, serialBytes); err != nil {
		return nil, fmt.Errorf("read certificate serial entropy: %w", err)
	}
	serialBytes[0] &= 0x7f
	serial := new(big.Int).SetBytes(serialBytes)
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}
	return serial, nil
}

func fingerprint(certificateDER []byte) string {
	digest := sha256.Sum256(certificateDER)
	encoded := strings.ToUpper(hex.EncodeToString(digest[:]))
	groups := make([]string, 0, len(encoded)/2)
	for index := 0; index < len(encoded); index += 2 {
		groups = append(groups, encoded[index:index+2])
	}
	return "SHA256:" + strings.Join(groups, ":")
}

func validServerName(name string) bool {
	if name == "" || len(name) > 253 || strings.ContainsAny(name, "/\\ ") {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
				(character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}
