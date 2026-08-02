package certificates

import (
	"context"
	"errors"
	"io"
	"syscall"
	"testing"
)

func TestHTTP01PortCheckerBindsPort80AndClosesListener(t *testing.T) {
	listener := &fakeHTTP01Listener{}
	binder := &fakeHTTP01Binder{listener: listener}
	checker, err := NewHTTP01PortChecker(binder)
	if err != nil {
		t.Fatalf("NewHTTP01PortChecker() error = %v", err)
	}

	available, err := checker.Available(context.Background())
	if err != nil || !available {
		t.Fatalf("Available() = %v, %v; want true, nil", available, err)
	}
	if binder.network != "tcp" || binder.address != ":80" || !listener.closed {
		t.Fatalf("bind = %q %q, closed = %v", binder.network, binder.address, listener.closed)
	}
}

func TestHTTP01PortCheckerTreatsAddressInUseAsUnavailable(t *testing.T) {
	checker, err := NewHTTP01PortChecker(&fakeHTTP01Binder{err: syscall.EADDRINUSE})
	if err != nil {
		t.Fatalf("NewHTTP01PortChecker() error = %v", err)
	}

	available, err := checker.Available(context.Background())
	if err != nil || available {
		t.Fatalf("Available() = %v, %v; want false, nil", available, err)
	}
}

func TestHTTP01PortCheckerPreservesProbeAndCloseErrors(t *testing.T) {
	probeErr := errors.New("probe failed")
	checker, _ := NewHTTP01PortChecker(&fakeHTTP01Binder{err: probeErr})
	if _, err := checker.Available(context.Background()); !errors.Is(err, probeErr) {
		t.Fatalf("Available() error = %v, want probe error", err)
	}

	closeErr := errors.New("close failed")
	checker, _ = NewHTTP01PortChecker(&fakeHTTP01Binder{listener: &fakeHTTP01Listener{err: closeErr}})
	if _, err := checker.Available(context.Background()); !errors.Is(err, closeErr) {
		t.Fatalf("Available() error = %v, want close error", err)
	}
}

func TestHTTP01PortCheckerRejectsUnsafeDependencies(t *testing.T) {
	if _, err := NewHTTP01PortChecker(nil); err == nil {
		t.Fatal("NewHTTP01PortChecker(nil) succeeded")
	}
	binder := &fakeHTTP01Binder{listener: &fakeHTTP01Listener{}}
	checker, _ := NewHTTP01PortChecker(binder)
	if _, err := checker.Available(nil); err == nil || binder.calls != 0 {
		t.Fatalf("Available(nil) error = %v, calls = %d", err, binder.calls)
	}
}

type fakeHTTP01Binder struct {
	listener io.Closer
	err      error
	network  string
	address  string
	calls    int
}

func (binder *fakeHTTP01Binder) Listen(_ context.Context, network string, address string) (io.Closer, error) {
	binder.calls++
	binder.network = network
	binder.address = address
	return binder.listener, binder.err
}

type fakeHTTP01Listener struct {
	err    error
	closed bool
}

func (listener *fakeHTTP01Listener) Close() error {
	listener.closed = true
	return listener.err
}
