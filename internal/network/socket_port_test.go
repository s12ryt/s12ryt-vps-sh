package network

import (
	"errors"
	"io"
	"reflect"
	"syscall"
	"testing"
)

func TestSocketPortCheckerBindsAndClosesRequestedTransport(t *testing.T) {
	tcp := &recordingSocketBinder{}
	udp := &recordingSocketBinder{}
	checker, err := NewSocketPortChecker(tcp.Bind, udp.Bind)
	if err != nil {
		t.Fatalf("NewSocketPortChecker() error = %v", err)
	}

	available, err := checker.Available("tcp", 23456)
	if err != nil || !available {
		t.Fatalf("TCP available = %v, error = %v", available, err)
	}
	available, err = checker.Available("udp", 23456)
	if err != nil || !available {
		t.Fatalf("UDP available = %v, error = %v", available, err)
	}

	if !reflect.DeepEqual(tcp.addresses, []string{":23456"}) {
		t.Fatalf("TCP addresses = %#v", tcp.addresses)
	}
	if !reflect.DeepEqual(udp.addresses, []string{":23456"}) {
		t.Fatalf("UDP addresses = %#v", udp.addresses)
	}
	if tcp.closes != 1 || udp.closes != 1 {
		t.Fatalf("close counts = TCP %d UDP %d, want 1 each", tcp.closes, udp.closes)
	}
}

func TestSocketPortCheckerTreatsAddressInUseAsUnavailable(t *testing.T) {
	tcp := &recordingSocketBinder{err: syscall.EADDRINUSE}
	checker, err := NewSocketPortChecker(tcp.Bind, (&recordingSocketBinder{}).Bind)
	if err != nil {
		t.Fatalf("NewSocketPortChecker() error = %v", err)
	}

	available, err := checker.Available("tcp", 23456)
	if err != nil {
		t.Fatalf("Available() error = %v", err)
	}
	if available {
		t.Fatal("address-in-use port reported available")
	}
}

func TestSocketPortCheckerReturnsUnexpectedBindAndCloseErrors(t *testing.T) {
	wantBindErr := errors.New("permission denied")
	checker, err := NewSocketPortChecker(
		(&recordingSocketBinder{err: wantBindErr}).Bind,
		(&recordingSocketBinder{}).Bind,
	)
	if err != nil {
		t.Fatalf("NewSocketPortChecker() error = %v", err)
	}
	if _, err := checker.Available("tcp", 23456); !errors.Is(err, wantBindErr) {
		t.Fatalf("bind error = %v, want errors.Is permission denied", err)
	}

	wantCloseErr := errors.New("close failed")
	checker, err = NewSocketPortChecker(
		(&recordingSocketBinder{closeErr: wantCloseErr}).Bind,
		(&recordingSocketBinder{}).Bind,
	)
	if err != nil {
		t.Fatalf("NewSocketPortChecker() error = %v", err)
	}
	if _, err := checker.Available("tcp", 23456); !errors.Is(err, wantCloseErr) {
		t.Fatalf("close error = %v, want errors.Is close failed", err)
	}
}

func TestSocketPortCheckerRejectsUnsafeInputsBeforeBinding(t *testing.T) {
	binder := &recordingSocketBinder{}
	if _, err := NewSocketPortChecker(nil, binder.Bind); err == nil {
		t.Fatal("nil TCP binder was accepted")
	}
	if _, err := NewSocketPortChecker(binder.Bind, nil); err == nil {
		t.Fatal("nil UDP binder was accepted")
	}

	checker, err := NewSocketPortChecker(binder.Bind, binder.Bind)
	if err != nil {
		t.Fatalf("NewSocketPortChecker() error = %v", err)
	}
	for _, input := range []struct {
		network string
		port    int
	}{
		{network: "sctp", port: 23456},
		{network: "tcp", port: 19999},
		{network: "udp", port: 50000},
	} {
		if _, err := checker.Available(input.network, input.port); err == nil {
			t.Fatalf("Available(%q, %d) accepted unsafe input", input.network, input.port)
		}
	}
	if len(binder.addresses) != 0 {
		t.Fatalf("unsafe inputs reached binder: %#v", binder.addresses)
	}
}

type recordingSocketBinder struct {
	addresses []string
	closes    int
	err       error
	closeErr  error
}

func (binder *recordingSocketBinder) Bind(address string) (io.Closer, error) {
	binder.addresses = append(binder.addresses, address)
	if binder.err != nil {
		return nil, binder.err
	}
	return socketCloser{close: func() error {
		binder.closes++
		return binder.closeErr
	}}, nil
}

type socketCloser struct {
	close func() error
}

func (closer socketCloser) Close() error {
	return closer.close()
}
