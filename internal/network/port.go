package network

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	minimumNodePort = 20000
	nodePortRange   = 30000
	portSampleLimit = 60000
)

var ErrNoAvailableNodePort = errors.New("no available node port found")

type PortAvailabilityChecker interface {
	Available(network string, port int) (bool, error)
}

func AllocateNodePort(entropy io.Reader, checker PortAvailabilityChecker, attempts int) (int, error) {
	if entropy == nil {
		return 0, errors.New("entropy reader is required")
	}
	if checker == nil {
		return 0, errors.New("port availability checker is required")
	}
	if attempts < 1 {
		return 0, errors.New("port allocation attempts must be positive")
	}

	for attempt := 0; attempt < attempts; attempt++ {
		var sample [2]byte
		if _, err := io.ReadFull(entropy, sample[:]); err != nil {
			return 0, fmt.Errorf("read port entropy: %w", err)
		}
		value := int(binary.BigEndian.Uint16(sample[:]))
		if value >= portSampleLimit {
			continue
		}
		port := minimumNodePort + value%nodePortRange
		tcpAvailable, err := checker.Available("tcp", port)
		if err != nil {
			return 0, fmt.Errorf("check TCP port %d: %w", port, err)
		}
		if !tcpAvailable {
			continue
		}
		udpAvailable, err := checker.Available("udp", port)
		if err != nil {
			return 0, fmt.Errorf("check UDP port %d: %w", port, err)
		}
		if udpAvailable {
			return port, nil
		}
	}

	return 0, ErrNoAvailableNodePort
}
