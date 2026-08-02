package certificates

import (
	"errors"
	"io"
	"io/fs"
	"net/mail"
	"net/netip"
	"time"
)

const ProjectTLSDirectory = "/opt/s12ryt-ipv6/tls"
const DeploymentSelfSigned = "self-signed"
const DeploymentACMEHTTP01 = "acme-http-01"
const DeploymentExternalCopy = "external-copy"
const DeploymentExternalReference = "external-reference"
const RenewalPreserveExisting = "preserve-existing"

const projectRoot = "/opt/s12ryt-ipv6"

type DeploymentOptions struct {
	Mode                string
	ProjectRoot         string
	CertificatePath     string
	PrivateKeyPath      string
	ServerName          string
	Email               string
	IPAddresses         []netip.Addr
	Now                 time.Time
	Entropy             io.Reader
	HTTPPort80Available bool
}

type DeploymentFile struct {
	Mode    fs.FileMode
	Content []byte
}

type DeploymentPlan struct {
	CertificatePath      string
	PrivateKeyPath       string
	FingerprintSHA256    string
	Files                map[string]DeploymentFile
	ChallengeAddress     string
	RenewBefore          time.Duration
	RenewalFailurePolicy string
}

func BuildDeploymentPlan(options DeploymentOptions) (DeploymentPlan, error) {
	if options.ProjectRoot == "" {
		options.ProjectRoot = projectRoot
	}
	if options.ProjectRoot != projectRoot {
		return DeploymentPlan{}, errors.New("certificate project root must be /opt/s12ryt-ipv6")
	}

	switch options.Mode {
	case DeploymentSelfSigned:
		return buildSelfSignedDeployment(options)
	case DeploymentACMEHTTP01:
		return buildACMEDeployment(options)
	case DeploymentExternalCopy, DeploymentExternalReference:
		return buildExternalDeployment(options)
	default:
		return DeploymentPlan{}, errors.New("unsupported certificate deployment mode")
	}
}

func buildSelfSignedDeployment(options DeploymentOptions) (DeploymentPlan, error) {
	serverNames := []string(nil)
	if options.ServerName != "" {
		serverNames = []string{options.ServerName}
	}
	material, err := GenerateSelfSigned(SelfSignedOptions{
		ServerNames: serverNames,
		IPAddresses: options.IPAddresses,
		Now:         options.Now,
		Entropy:     options.Entropy,
	})
	if err != nil {
		return DeploymentPlan{}, err
	}
	certificatePath := ProjectTLSDirectory + "/server.crt"
	privateKeyPath := ProjectTLSDirectory + "/server.key"
	return DeploymentPlan{
		CertificatePath:   certificatePath,
		PrivateKeyPath:    privateKeyPath,
		FingerprintSHA256: material.FingerprintSHA256,
		Files: map[string]DeploymentFile{
			certificatePath: {Mode: 0o644, Content: material.CertificatePEM},
			privateKeyPath:  {Mode: 0o600, Content: material.PrivateKeyPEM},
		},
	}, nil
}

func buildACMEDeployment(options DeploymentOptions) (DeploymentPlan, error) {
	if options.Now.IsZero() {
		return DeploymentPlan{}, errors.New("ACME validation time is required")
	}
	if !validServerName(options.ServerName) {
		return DeploymentPlan{}, errors.New("ACME requires a valid domain name")
	}
	if options.Email != "" {
		address, err := mail.ParseAddress(options.Email)
		if err != nil || address.Address != options.Email {
			return DeploymentPlan{}, errors.New("ACME email address is invalid")
		}
	}
	if !options.HTTPPort80Available {
		return DeploymentPlan{}, errors.New("ACME HTTP-01 requires available TCP port 80")
	}
	return DeploymentPlan{
		CertificatePath:      ProjectTLSDirectory + "/acme.crt",
		PrivateKeyPath:       ProjectTLSDirectory + "/acme.key",
		Files:                map[string]DeploymentFile{},
		ChallengeAddress:     ":80",
		RenewBefore:          30 * 24 * time.Hour,
		RenewalFailurePolicy: RenewalPreserveExisting,
	}, nil
}

func buildExternalDeployment(options DeploymentOptions) (DeploymentPlan, error) {
	certificatePEM, err := readRegularFile(options.CertificatePath, false)
	if err != nil {
		return DeploymentPlan{}, err
	}
	privateKeyPEM, err := readRegularFile(options.PrivateKeyPath, true)
	if err != nil {
		return DeploymentPlan{}, err
	}
	info, err := ValidatePEM(certificatePEM, privateKeyPEM, options.Now, options.ServerName)
	if err != nil {
		return DeploymentPlan{}, err
	}
	if options.Mode == DeploymentExternalReference {
		return DeploymentPlan{
			CertificatePath:   options.CertificatePath,
			PrivateKeyPath:    options.PrivateKeyPath,
			FingerprintSHA256: info.FingerprintSHA256,
			Files:             map[string]DeploymentFile{},
		}, nil
	}

	certificatePath := ProjectTLSDirectory + "/server.crt"
	privateKeyPath := ProjectTLSDirectory + "/server.key"
	return DeploymentPlan{
		CertificatePath:   certificatePath,
		PrivateKeyPath:    privateKeyPath,
		FingerprintSHA256: info.FingerprintSHA256,
		Files: map[string]DeploymentFile{
			certificatePath: {Mode: 0o644, Content: certificatePEM},
			privateKeyPath:  {Mode: 0o600, Content: privateKeyPEM},
		},
	}, nil
}
