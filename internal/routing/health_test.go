package routing

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestHealthMonitorChecksEveryCandidateWithBoundedTimeout(t *testing.T) {
	start := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	selector := mustSelector(t, start)
	probe := &recordingProbe{
		results: map[string]error{
			"preferred": errors.New("unreachable"),
			"fallback":  nil,
		},
	}
	monitor, err := NewHealthMonitor(HealthMonitorOptions{
		Selector:   selector,
		Candidates: []string{"preferred", "fallback"},
		URL:        "https://www.cloudflare.com/cdn-cgi/trace",
		Interval:   30 * time.Second,
		Timeout:    5 * time.Second,
		Probe:      probe,
	})
	if err != nil {
		t.Fatalf("NewHealthMonitor() error = %v", err)
	}

	for range 3 {
		if err := monitor.CheckOnce(context.Background()); err != nil {
			t.Fatalf("CheckOnce() error = %v", err)
		}
	}

	if got := selector.Select(start); got != "fallback" {
		t.Fatalf("selector after failures = %q, want fallback", got)
	}
	checks := probe.snapshot()
	if len(checks) != 6 {
		t.Fatalf("probe checks = %d, want 6", len(checks))
	}
	for _, check := range checks {
		if check.url != "https://www.cloudflare.com/cdn-cgi/trace" {
			t.Fatalf("probe URL = %q", check.url)
		}
		if check.timeout <= 0 || check.timeout > 5*time.Second {
			t.Fatalf("probe timeout = %v, want within five seconds", check.timeout)
		}
	}
}

func TestHealthMonitorRestoresPreferredAfterConsecutiveSuccesses(t *testing.T) {
	start := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	selector := mustSelector(t, start)
	probe := &recordingProbe{results: map[string]error{"preferred": errors.New("down")}}
	monitor := mustHealthMonitor(t, selector, probe, nil)

	for range 3 {
		if err := monitor.CheckOnce(context.Background()); err != nil {
			t.Fatalf("CheckOnce(failure) error = %v", err)
		}
	}
	if got := selector.Select(start); got != "fallback" {
		t.Fatalf("selector after failures = %q", got)
	}

	probe.setResult("preferred", nil)
	for range 3 {
		if err := monitor.CheckOnce(context.Background()); err != nil {
			t.Fatalf("CheckOnce(recovery) error = %v", err)
		}
	}
	if got := selector.Select(start); got != "preferred" {
		t.Fatalf("selector after recovery = %q", got)
	}
}

func TestHealthMonitorRunsImmediatelyThenOnConfiguredTicks(t *testing.T) {
	start := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	selector := mustSelector(t, start)
	probe := &recordingProbe{results: map[string]error{}}
	ticker := newFakeHealthTicker()
	monitor := mustHealthMonitor(t, selector, probe, func(interval time.Duration) healthTicker {
		if interval != 30*time.Second {
			t.Fatalf("ticker interval = %v", interval)
		}
		return ticker
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- monitor.Run(ctx) }()

	probe.waitForChecks(t, 2)
	ticker.tick <- start.Add(30 * time.Second)
	probe.waitForChecks(t, 4)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
	if !ticker.stopped {
		t.Fatal("Run() did not stop its ticker")
	}
}

func TestHealthMonitorRejectsUnsafeOptions(t *testing.T) {
	start := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	selector := mustSelector(t, start)
	probe := &recordingProbe{results: map[string]error{}}
	tests := []HealthMonitorOptions{
		{Candidates: []string{"preferred"}, URL: "https://example.com", Interval: 30 * time.Second, Timeout: 5 * time.Second, Probe: probe},
		{Selector: selector, URL: "https://example.com", Interval: 30 * time.Second, Timeout: 5 * time.Second, Probe: probe},
		{Selector: selector, Candidates: []string{"preferred", "preferred"}, URL: "https://example.com", Interval: 30 * time.Second, Timeout: 5 * time.Second, Probe: probe},
		{Selector: selector, Candidates: []string{"preferred"}, URL: "http://example.com", Interval: 30 * time.Second, Timeout: 5 * time.Second, Probe: probe},
		{Selector: selector, Candidates: []string{"preferred"}, URL: "https://example.com", Timeout: 5 * time.Second, Probe: probe},
		{Selector: selector, Candidates: []string{"preferred"}, URL: "https://example.com", Interval: 30 * time.Second, Probe: probe},
		{Selector: selector, Candidates: []string{"preferred"}, URL: "https://example.com", Interval: 30 * time.Second, Timeout: 5 * time.Second},
	}
	for index, options := range tests {
		if _, err := NewHealthMonitor(options); err == nil {
			t.Fatalf("NewHealthMonitor() accepted unsafe case %d", index)
		}
	}
}

type probeCheck struct {
	candidate string
	url       string
	timeout   time.Duration
}

type recordingProbe struct {
	mu      sync.Mutex
	checks  []probeCheck
	results map[string]error
}

func (probe *recordingProbe) Probe(ctx context.Context, candidate, url string) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("probe context has no deadline")
	}
	probe.mu.Lock()
	probe.checks = append(probe.checks, probeCheck{candidate: candidate, url: url, timeout: time.Until(deadline)})
	result := probe.results[candidate]
	probe.mu.Unlock()
	return result
}

func (probe *recordingProbe) setResult(candidate string, result error) {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	probe.results[candidate] = result
}

func (probe *recordingProbe) snapshot() []probeCheck {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	return append([]probeCheck(nil), probe.checks...)
}

func (probe *recordingProbe) waitForChecks(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(probe.snapshot()) >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("probe checks = %d, want at least %d", len(probe.snapshot()), count)
}

type fakeHealthTicker struct {
	tick    chan time.Time
	stopped bool
}

func newFakeHealthTicker() *fakeHealthTicker {
	return &fakeHealthTicker{tick: make(chan time.Time, 1)}
}

func (ticker *fakeHealthTicker) C() <-chan time.Time { return ticker.tick }
func (ticker *fakeHealthTicker) Stop()               { ticker.stopped = true }

func mustHealthMonitor(t *testing.T, selector *Selector, probe Probe, factory healthTickerFactory) *HealthMonitor {
	t.Helper()
	monitor, err := NewHealthMonitor(HealthMonitorOptions{
		Selector:   selector,
		Candidates: []string{"preferred", "fallback"},
		URL:        "https://www.cloudflare.com/cdn-cgi/trace",
		Interval:   30 * time.Second,
		Timeout:    5 * time.Second,
		Probe:      probe,
		newTicker:  factory,
	})
	if err != nil {
		t.Fatalf("NewHealthMonitor() error = %v", err)
	}
	return monitor
}
