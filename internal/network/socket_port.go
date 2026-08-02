package network

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"syscall"
)

type SocketBinder func(address string) (io.Closer, error)

type SocketPortChecker struct {
	tcpBinder SocketBinder
	udpBinder SocketBinder
}

func NewSocketPortChecker(tcpBinder SocketBinder, udpBinder SocketBinder) (*SocketPortChecker, error) {
	if tcpBinder == nil {
		return nil, errors.New("TCP socket binder is required")
	}
	if udpBinder == nil {
		return nil, errors.New("UDP socket binder is required")
	}
	return &SocketPortChecker{tcpBinder: tcpBinder, udpBinder: udpBinder}, nil
}

func NewSystemSocketPortChecker() *SocketPortChecker {
	return &SocketPortChecker{
		tcpBinder: func(address string) (io.Closer, error) {
			return net.Listen("tcp", address)
		},
		udpBinder: func(address string) (io.Closer, error) {
			return net.ListenPacket("udp", address)
		},
	}
}

func (checker *SocketPortChecker) Available(network string, port int) (bool, error) {
	if checker == nil {
		return false, errors.New("socket port checker is required")
	}
	if port < minimumNodePort || port >= minimumNodePort+nodePortRange {
		return false, fmt.Errorf("port %d is outside the node port range", port)
	}

	var binder SocketBinder
	switch network {
	case "tcp":
		binder = checker.tcpBinder
	case "udp":
		binder = checker.udpBinder
	default:
		return false, fmt.Errorf("unsupported socket network %q", network)
	}

	address := ":" + strconv.Itoa(port)
	socket, err := binder(address)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return false, nil
		}
		return false, fmt.Errorf("bind %s socket %s: %w", network, address, err)
	}
	if socket == nil {
		return false, errors.New("socket binder returned a nil socket")
	}
	if err := socket.Close(); err != nil {
		return false, fmt.Errorf("close %s socket %s: %w", network, address, err)
	}
	return true, nil
}
