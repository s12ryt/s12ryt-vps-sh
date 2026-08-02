package nodes

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/s12ryt/s12ryt-vps-sh/internal/domain"
)

func TestManagerCreatesNodeWithAutomaticPortAndUniqueCredential(t *testing.T) {
	store := &recordingStore{}
	allocatorCalls := 0
	manager, err := NewManager(ManagerOptions{
		Config:       domain.DefaultConfig(),
		Store:        store,
		Entropy:      bytes.NewReader(bytes.Repeat([]byte{0x31}, 128)),
		AllocatePort: func() (int, error) {
			allocatorCalls++
			return 24443, nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	node, err := manager.Create(CreateInput{ID: "edge-vless", Protocol: domain.ProtocolVLESS, Enabled: true})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if allocatorCalls != 1 || node.Port != 24443 || !node.Enabled {
		t.Fatalf("created node = %#v, allocator calls = %d", node, allocatorCalls)
	}
	if err := node.Credential.Validate(domain.ProtocolVLESS); err != nil {
		t.Fatalf("generated credential is invalid: %v", err)
	}
	if len(store.saved) != 1 || !reflect.DeepEqual(store.saved[0].Nodes, []domain.Node{node}) {
		t.Fatalf("saved configs = %#v", store.saved)
	}
	if snapshot := manager.Snapshot(); !reflect.DeepEqual(snapshot.Nodes, []domain.Node{node}) {
		t.Fatalf("snapshot = %#v", snapshot.Nodes)
	}
}

func TestManagerAcceptsManualPortWithoutCallingAllocator(t *testing.T) {
	store := &recordingStore{}
	manager := newTestManager(t, store, func() (int, error) {
		t.Fatal("automatic allocator called for a manual port")
		return 0, nil
	})
	node, err := manager.Create(CreateInput{
		ID: "manual-socks", Protocol: domain.ProtocolSOCKS5, Port: 25555, Enabled: true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if node.Port != 25555 {
		t.Fatalf("port = %d, want 25555", node.Port)
	}
}

func TestManagerRejectsDuplicateAndInvalidNodesWithoutSaving(t *testing.T) {
	store := &recordingStore{}
	manager := newTestManager(t, store, func() (int, error) { return 24443, nil })
	if _, err := manager.Create(CreateInput{ID: "node-one", Protocol: domain.ProtocolVLESS}); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	savesAfterFirst := len(store.saved)

	for _, input := range []CreateInput{
		{ID: "node-one", Protocol: domain.ProtocolVMess},
		{ID: "../unsafe", Protocol: domain.ProtocolVLESS},
		{ID: "unknown", Protocol: domain.Protocol("unknown")},
		{ID: "bad-port", Protocol: domain.ProtocolVLESS, Port: 443},
	} {
		if _, err := manager.Create(input); err == nil {
			t.Fatalf("Create(%#v) succeeded", input)
		}
	}
	if len(store.saved) != savesAfterFirst {
		t.Fatalf("invalid creates changed save count from %d to %d", savesAfterFirst, len(store.saved))
	}
}

func TestManagerUpdatePreservesProtocolAndCredential(t *testing.T) {
	store := &recordingStore{}
	manager := newTestManager(t, store, func() (int, error) { return 24443, nil })
	created, err := manager.Create(CreateInput{ID: "edge-tuic", Protocol: domain.ProtocolTUIC, Enabled: true})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	updated, err := manager.Update(UpdateInput{ID: created.ID, Port: 26666, Enabled: false})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Protocol != created.Protocol || updated.Credential != created.Credential {
		t.Fatalf("update replaced immutable protocol or credential: %#v", updated)
	}
	if updated.Port != 26666 || updated.Enabled {
		t.Fatalf("updated node = %#v", updated)
	}
	if _, err := manager.Update(UpdateInput{ID: "missing", Port: 27777, Enabled: true}); err == nil {
		t.Fatal("Update() accepted a missing node")
	}
}

func TestManagerDeletePersistsAndRejectsMissingNode(t *testing.T) {
	store := &recordingStore{}
	manager := newTestManager(t, store, func() (int, error) { return 24443, nil })
	if _, err := manager.Create(CreateInput{ID: "edge-anytls", Protocol: domain.ProtocolAnyTLS}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := manager.Delete("edge-anytls"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if len(manager.Snapshot().Nodes) != 0 || len(store.saved[len(store.saved)-1].Nodes) != 0 {
		t.Fatal("deleted node remains in state")
	}
	if err := manager.Delete("edge-anytls"); err == nil {
		t.Fatal("Delete() accepted a missing node")
	}
}

func TestManagerKeepsCurrentStateWhenPersistenceFails(t *testing.T) {
	wantErr := errors.New("disk full")
	store := &recordingStore{err: wantErr}
	manager := newTestManager(t, store, func() (int, error) { return 24443, nil })
	if _, err := manager.Create(CreateInput{ID: "edge-vmess", Protocol: domain.ProtocolVMess}); !errors.Is(err, wantErr) {
		t.Fatalf("Create() error = %v, want persistence error", err)
	}
	if len(manager.Snapshot().Nodes) != 0 {
		t.Fatal("failed persistence changed current state")
	}
}

func newTestManager(t *testing.T, store ConfigStore, allocator func() (int, error)) *Manager {
	t.Helper()
	manager, err := NewManager(ManagerOptions{
		Config:       domain.DefaultConfig(),
		Store:        store,
		Entropy:      bytes.NewReader(bytes.Repeat([]byte{0x41}, 1024)),
		AllocatePort: allocator,
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager
}

type recordingStore struct {
	saved []domain.Config
	err   error
}

func (store *recordingStore) Save(config domain.Config) error {
	if store.err != nil {
		return store.err
	}
	store.saved = append(store.saved, cloneTestConfig(config))
	return nil
}

func cloneTestConfig(config domain.Config) domain.Config {
	config.Panel.AllowedCIDRs = append([]string(nil), config.Panel.AllowedCIDRs...)
	config.Nodes = append([]domain.Node(nil), config.Nodes...)
	return config
}
