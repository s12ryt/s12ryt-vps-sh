package networksetup

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	projectnetwork "github.com/s12ryt/s12ryt-vps-sh/internal/network"
)

type OutputRunner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type IPDiscovery struct {
	runner OutputRunner
}

func NewIPDiscovery(runner OutputRunner) (*IPDiscovery, error) {
	if runner == nil {
		return nil, errors.New("IP discovery output runner is required")
	}
	return &IPDiscovery{runner: runner}, nil
}

func (discovery *IPDiscovery) GlobalIPv6Addresses(ctx context.Context) ([]projectnetwork.InterfaceAddress, error) {
	if ctx == nil {
		return nil, errors.New("IP address discovery context is required")
	}
	payload, err := discovery.runner.Output(ctx, "ip", "-j", "-6", "addr", "show")
	if err != nil {
		return nil, fmt.Errorf("query global IPv6 addresses: %w", err)
	}
	addresses, err := projectnetwork.ParseGlobalIPv6Addresses(payload)
	if err != nil {
		return nil, fmt.Errorf("parse global IPv6 addresses: %w", err)
	}
	return addresses, nil
}

func (discovery *IPDiscovery) UniqueIPv6Gateway(ctx context.Context, interfaceName string) (netip.Addr, error) {
	if ctx == nil {
		return netip.Addr{}, errors.New("IP route discovery context is required")
	}
	if interfaceName == "" || !interfaceNamePattern.MatchString(interfaceName) {
		return netip.Addr{}, errors.New("IP route interface name is invalid")
	}
	payload, err := discovery.runner.Output(ctx, "ip", "-j", "-6", "route", "show", "default")
	if err != nil {
		return netip.Addr{}, fmt.Errorf("query IPv6 default route: %w", err)
	}
	gateway, err := projectnetwork.ParseUniqueIPv6Gateway(payload, interfaceName)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parse IPv6 default route: %w", err)
	}
	return gateway, nil
}
