package certificates

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
)

type HTTP01PortBinder interface {
	Listen(context.Context, string, string) (io.Closer, error)
}

type HTTP01PortChecker struct {
	binder HTTP01PortBinder
}

func NewHTTP01PortChecker(binder HTTP01PortBinder) (*HTTP01PortChecker, error) {
	if binder == nil {
		return nil, fmt.Errorf("HTTP-01 port binder is required")
	}
	return &HTTP01PortChecker{binder: binder}, nil
}

func NewSystemHTTP01PortChecker() (*HTTP01PortChecker, error) {
	return NewHTTP01PortChecker(systemHTTP01PortBinder{})
}

func (checker *HTTP01PortChecker) Available(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("HTTP-01 probe context is required")
	}
	listener, err := checker.binder.Listen(ctx, "tcp", ":80")
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return false, nil
		}
		return false, fmt.Errorf("probe TCP port 80: %w", err)
	}
	if listener == nil {
		return false, fmt.Errorf("probe TCP port 80 returned a nil listener")
	}
	if err := listener.Close(); err != nil {
		return false, fmt.Errorf("close TCP port 80 probe: %w", err)
	}
	return true, nil
}

type systemHTTP01PortBinder struct{}

func (systemHTTP01PortBinder) Listen(ctx context.Context, network string, address string) (io.Closer, error) {
	var config net.ListenConfig
	return config.Listen(ctx, network, address)
}
