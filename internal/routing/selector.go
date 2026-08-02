package routing

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type SelectorOptions struct {
	Candidates        []string
	RotationInterval  time.Duration
	FailureThreshold  int
	RecoveryThreshold int
	StartIndex        int
	Now               time.Time
}

type healthState struct {
	failures  int
	successes int
}

type Selector struct {
	mu                sync.Mutex
	candidates        []string
	indices           map[string]int
	health            map[string]healthState
	current           int
	preferred         int
	rotationInterval  time.Duration
	failureThreshold  int
	recoveryThreshold int
	lastRotation      time.Time
}

func NewSelector(options SelectorOptions) (*Selector, error) {
	if len(options.Candidates) == 0 {
		return nil, errors.New("at least one outbound candidate is required")
	}
	if options.RotationInterval <= 0 {
		return nil, errors.New("rotation interval must be positive")
	}
	if options.FailureThreshold <= 0 || options.RecoveryThreshold <= 0 {
		return nil, errors.New("health thresholds must be positive")
	}
	if options.StartIndex < 0 {
		return nil, errors.New("start index cannot be negative")
	}

	candidates := append([]string(nil), options.Candidates...)
	indices := make(map[string]int, len(candidates))
	health := make(map[string]healthState, len(candidates))
	for index, candidate := range candidates {
		if candidate == "" {
			return nil, errors.New("outbound candidate cannot be empty")
		}
		if _, duplicate := indices[candidate]; duplicate {
			return nil, fmt.Errorf("duplicate outbound candidate %q", candidate)
		}
		indices[candidate] = index
		health[candidate] = healthState{}
	}

	startIndex := options.StartIndex % len(candidates)
	return &Selector{
		candidates:        candidates,
		indices:           indices,
		health:            health,
		current:           startIndex,
		preferred:         startIndex,
		rotationInterval:  options.RotationInterval,
		failureThreshold:  options.FailureThreshold,
		recoveryThreshold: options.RecoveryThreshold,
		lastRotation:      options.Now,
	}, nil
}

func (selector *Selector) Select(now time.Time) string {
	selector.mu.Lock()
	defer selector.mu.Unlock()

	if now.After(selector.lastRotation) || now.Equal(selector.lastRotation) {
		elapsed := now.Sub(selector.lastRotation)
		steps := int(elapsed / selector.rotationInterval)
		if steps > 0 {
			selector.current = (selector.current + steps) % len(selector.candidates)
			selector.lastRotation = selector.lastRotation.Add(time.Duration(steps) * selector.rotationInterval)
		}
	}
	return selector.candidates[selector.current]
}

func (selector *Selector) RecordHealth(candidate string, healthy bool) error {
	selector.mu.Lock()
	defer selector.mu.Unlock()

	index, exists := selector.indices[candidate]
	if !exists {
		return fmt.Errorf("unknown outbound candidate %q", candidate)
	}
	state := selector.health[candidate]
	if healthy {
		state.failures = 0
		state.successes++
		selector.health[candidate] = state
		if index == selector.preferred && selector.current != selector.preferred && state.successes >= selector.recoveryThreshold {
			selector.current = selector.preferred
		}
		return nil
	}

	state.successes = 0
	state.failures++
	selector.health[candidate] = state
	if index == selector.current && state.failures >= selector.failureThreshold {
		selector.current = (selector.current + 1) % len(selector.candidates)
	}
	return nil
}
