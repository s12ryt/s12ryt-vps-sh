package nodes

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/s12ryt/s12ryt-vps-sh/internal/importer"
	"github.com/s12ryt/s12ryt-vps-sh/internal/runtimeconfig"
)

type ImportRemoteInput struct {
	Payload        []byte
	AllowIPv4Proxy bool
	Enabled        bool
}

type RemoteOutboundSummary struct {
	Tag                  string
	Type                 string
	Server               string
	Port                 int
	Enabled              bool
	IPv4FallbackPosition int
}

func (manager *Manager) RemoteOutbounds() []RemoteOutboundSummary {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return summarizeRemoteOutbounds(manager.runtimeState)
}

func (manager *Manager) ImportRemoteOutbounds(input ImportRemoteInput) ([]RemoteOutboundSummary, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.deploymentApplier == nil {
		return nil, errors.New("remote outbounds require managed runtime state")
	}
	imported, err := importer.Import(input.Payload, importer.Options{AllowIPv4Proxy: input.AllowIPv4Proxy})
	if err != nil {
		return nil, fmt.Errorf("import remote outbounds: %w", err)
	}
	candidate := cloneRuntimeState(manager.runtimeState)
	start := len(candidate.RemoteOutbounds)
	for _, outbound := range imported {
		encoded, err := json.Marshal(outbound.Raw)
		if err != nil {
			return nil, fmt.Errorf("encode remote outbound %q: %w", outbound.Tag, err)
		}
		candidate.RemoteOutbounds = append(candidate.RemoteOutbounds, runtimeconfig.PersistedRemoteOutbound{
			Enabled: input.Enabled,
			Config:  encoded,
		})
	}
	if err := manager.persist(cloneConfig(manager.config), candidate); err != nil {
		return nil, err
	}
	summaries := summarizeRemoteOutbounds(manager.runtimeState)
	return append([]RemoteOutboundSummary(nil), summaries[start:]...), nil
}

func (manager *Manager) UpdateRemoteOutbound(tag string, enabled bool) (RemoteOutboundSummary, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.deploymentApplier == nil {
		return RemoteOutboundSummary{}, errors.New("remote outbounds require managed runtime state")
	}
	candidate := cloneRuntimeState(manager.runtimeState)
	for index := range candidate.RemoteOutbounds {
		identity, err := decodeRemoteIdentity(candidate.RemoteOutbounds[index].Config)
		if err != nil {
			return RemoteOutboundSummary{}, err
		}
		if identity.Tag != tag {
			continue
		}
		candidate.RemoteOutbounds[index].Enabled = enabled
		if err := manager.persist(cloneConfig(manager.config), candidate); err != nil {
			return RemoteOutboundSummary{}, err
		}
		return summarizeRemoteOutbounds(manager.runtimeState)[index], nil
	}
	return RemoteOutboundSummary{}, fmt.Errorf("remote outbound %q does not exist", tag)
}

func (manager *Manager) DeleteRemoteOutbound(tag string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.deploymentApplier == nil {
		return errors.New("remote outbounds require managed runtime state")
	}
	candidate := cloneRuntimeState(manager.runtimeState)
	for index := range candidate.RemoteOutbounds {
		identity, err := decodeRemoteIdentity(candidate.RemoteOutbounds[index].Config)
		if err != nil {
			return err
		}
		if identity.Tag != tag {
			continue
		}
		candidate.RemoteOutbounds = append(candidate.RemoteOutbounds[:index], candidate.RemoteOutbounds[index+1:]...)
		return manager.persist(cloneConfig(manager.config), candidate)
	}
	return fmt.Errorf("remote outbound %q does not exist", tag)
}

func (manager *Manager) SetIPv4Fallback(tags []string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.deploymentApplier == nil {
		return errors.New("IPv4 fallback requires managed runtime state")
	}
	candidate := cloneRuntimeState(manager.runtimeState)
	candidate.IPv4Fallback = append([]string(nil), tags...)
	return manager.persist(cloneConfig(manager.config), candidate)
}

type remoteIdentity struct {
	Tag    string `json:"tag"`
	Type   string `json:"type"`
	Server string `json:"server"`
	Port   int    `json:"server_port"`
}

func decodeRemoteIdentity(raw json.RawMessage) (remoteIdentity, error) {
	var identity remoteIdentity
	if err := json.Unmarshal(raw, &identity); err != nil {
		return remoteIdentity{}, fmt.Errorf("decode protected remote outbound: %w", err)
	}
	if identity.Tag == "" || identity.Type == "" || identity.Server == "" || identity.Port < 1 {
		return remoteIdentity{}, errors.New("protected remote outbound identity is incomplete")
	}
	return identity, nil
}

func summarizeRemoteOutbounds(state runtimeconfig.DeploymentState) []RemoteOutboundSummary {
	fallbackPositions := make(map[string]int, len(state.IPv4Fallback))
	for index, tag := range state.IPv4Fallback {
		fallbackPositions[tag] = index + 1
	}
	result := make([]RemoteOutboundSummary, 0, len(state.RemoteOutbounds))
	for _, persisted := range state.RemoteOutbounds {
		identity, err := decodeRemoteIdentity(persisted.Config)
		if err != nil {
			continue
		}
		result = append(result, RemoteOutboundSummary{
			Tag:                  identity.Tag,
			Type:                 identity.Type,
			Server:               identity.Server,
			Port:                 identity.Port,
			Enabled:              persisted.Enabled,
			IPv4FallbackPosition: fallbackPositions[identity.Tag],
		})
	}
	return result
}
