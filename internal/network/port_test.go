package network

import (
	"bytes"
	"errors"
	"reflect"
	"strconv"
	"testing"
)

func TestAllocateNodePortChecksTCPAndUDPAndSkipsOccupiedPorts(t *testing.T) {
	checker := &recordingPortChecker{
		availability: map[string]bool{
			"tcp:20000": false,
			"tcp:20001": true,
			"udp:20001": true,
		},
	}
	port, err := AllocateNodePort(bytes.NewReader([]byte{0, 0, 0, 1}), checker, 4)
	if err != nil {
		t.Fatalf("AllocateNodePort() error = %v", err)
	}
	if port != 20001 {
		t.Fatalf("port = %d, want 20001", port)
	}
	wantChecks := []string{"tcp:20000", "tcp:20001", "udp:20001"}
	if !reflect.DeepEqual(checker.checks, wantChecks) {
		t.Fatalf("checks = %#v, want %#v", checker.checks, wantChecks)
	}
}

func TestAllocateNodePortUsesRejectionSampling(t *testing.T) {
	checker := &recordingPortChecker{availability: map[string]bool{
		"tcp:20002": true,
		"udp:20002": true,
	}}
	port, err := AllocateNodePort(bytes.NewReader([]byte{0xea, 0x60, 0, 2}), checker, 2)
	if err != nil {
		t.Fatalf("AllocateNodePort() error = %v", err)
	}
	if port != 20002 {
		t.Fatalf("port = %d, want 20002", port)
	}
}

func TestAllocateNodePortReturnsCheckerErrors(t *testing.T) {
	wantErr := errors.New("socket check failed")
	checker := &recordingPortChecker{err: wantErr}
	_, err := AllocateNodePort(bytes.NewReader([]byte{0, 0}), checker, 1)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want errors.Is socket failure", err)
	}
}

func TestAllocateNodePortRejectsUnsafeInputsAndExhaustion(t *testing.T) {
	available := &recordingPortChecker{availability: map[string]bool{"tcp:20000": true, "udp:20000": true}}
	if _, err := AllocateNodePort(nil, available, 1); err == nil {
		t.Fatal("nil entropy was accepted")
	}
	if _, err := AllocateNodePort(bytes.NewReader([]byte{0, 0}), nil, 1); err == nil {
		t.Fatal("nil checker was accepted")
	}
	if _, err := AllocateNodePort(bytes.NewReader([]byte{0, 0}), available, 0); err == nil {
		t.Fatal("zero attempts was accepted")
	}

	occupied := &recordingPortChecker{availability: map[string]bool{"tcp:20000": false}}
	if _, err := AllocateNodePort(bytes.NewReader([]byte{0, 0}), occupied, 1); !errors.Is(err, ErrNoAvailableNodePort) {
		t.Fatalf("exhaustion error = %v", err)
	}
}

type recordingPortChecker struct {
	availability map[string]bool
	checks       []string
	err          error
}

func (checker *recordingPortChecker) Available(network string, port int) (bool, error) {
	key := network + ":" + strconv.Itoa(port)
	checker.checks = append(checker.checks, key)
	if checker.err != nil {
		return false, checker.err
	}
	return checker.availability[key], nil
}
