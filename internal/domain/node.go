package domain

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
)

const nodeUsernameLength = 12
const nodePasswordLength = 24
const shadowsocksDefaultMethod = "aes-256-gcm"

var nodeSecretPattern = regexp.MustCompile(`^[A-Za-z0-9]+$`)
var nodeUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type NodeCredential struct {
	Username string `json:"username,omitempty"`
	UUID     string `json:"uuid,omitempty"`
	Password string `json:"password,omitempty"`
	Method   string `json:"method,omitempty"`
}

func GenerateNodeCredential(protocol Protocol, entropy io.Reader) (NodeCredential, error) {
	if !supportedProtocol(protocol) {
		return NodeCredential{}, fmt.Errorf("unsupported protocol %q", protocol)
	}
	if entropy == nil {
		return NodeCredential{}, ErrInsufficientEntropy
	}

	credential := NodeCredential{}
	var err error
	switch protocol {
	case ProtocolVLESS, ProtocolVMess:
		credential.UUID, err = randomUUID(entropy)
	case ProtocolHysteria2, ProtocolAnyTLS:
		credential.Password, err = randomAlphaNumeric(entropy, nodePasswordLength)
	case ProtocolTUIC:
		credential.UUID, err = randomUUID(entropy)
		if err == nil {
			credential.Password, err = randomAlphaNumeric(entropy, nodePasswordLength)
		}
	case ProtocolSOCKS5:
		credential.Username, err = randomAlphaNumeric(entropy, nodeUsernameLength)
		if err == nil {
			credential.Password, err = randomAlphaNumeric(entropy, nodePasswordLength)
		}
	case ProtocolShadowsocks:
		credential.Method = shadowsocksDefaultMethod
		credential.Password, err = randomAlphaNumeric(entropy, nodePasswordLength)
	}
	if err != nil {
		return NodeCredential{}, err
	}
	return credential, nil
}

func (credential NodeCredential) Validate(protocol Protocol) error {
	if !supportedProtocol(protocol) {
		return fmt.Errorf("unsupported protocol %q", protocol)
	}

	switch protocol {
	case ProtocolVLESS, ProtocolVMess:
		if !nodeUUIDPattern.MatchString(credential.UUID) || credential.Username != "" || credential.Password != "" || credential.Method != "" {
			return errors.New("protocol requires only a valid UUID")
		}
	case ProtocolHysteria2, ProtocolAnyTLS:
		if !validNodeSecret(credential.Password, nodePasswordLength) || credential.Username != "" || credential.UUID != "" || credential.Method != "" {
			return errors.New("protocol requires only a 24-character password")
		}
	case ProtocolTUIC:
		if !nodeUUIDPattern.MatchString(credential.UUID) || !validNodeSecret(credential.Password, nodePasswordLength) || credential.Username != "" || credential.Method != "" {
			return errors.New("TUIC requires a UUID and 24-character password")
		}
	case ProtocolSOCKS5:
		if !validNodeSecret(credential.Username, nodeUsernameLength) || !validNodeSecret(credential.Password, nodePasswordLength) || credential.UUID != "" || credential.Method != "" {
			return errors.New("SOCKS5 requires a 12-character username and 24-character password")
		}
	case ProtocolShadowsocks:
		if credential.Method != shadowsocksDefaultMethod || !validNodeSecret(credential.Password, nodePasswordLength) || credential.Username != "" || credential.UUID != "" {
			return errors.New("Shadowsocks requires aes-256-gcm and a 24-character password")
		}
	}
	return nil
}

func randomUUID(entropy io.Reader) (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(entropy, value); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInsufficientEntropy, err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

func validNodeSecret(value string, length int) bool {
	return len(value) == length && nodeSecretPattern.MatchString(value)
}
