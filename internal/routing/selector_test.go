package routing

import (
	"testing"
	"time"
)

func TestSelectorRotatesOnlyWhenSelectingAfterInterval(t *testing.T) {
	start := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	selector, err := NewSelector(SelectorOptions{
		Candidates:        []string{"direct-v6-1", "direct-v6-2", "remote-1"},
		RotationInterval: time.Hour,
		FailureThreshold: 3,
		RecoveryThreshold: 3,
		StartIndex:        0,
		Now:               start,
	})
	if err != nil {
		t.Fatalf("NewSelector() error = %v", err)
	}

	if got := selector.Select(start.Add(59 * time.Minute)); got != "direct-v6-1" {
		t.Fatalf("Select() before interval = %q", got)
	}
	if got := selector.Select(start.Add(time.Hour)); got != "direct-v6-2" {
		t.Fatalf("Select() at interval = %q", got)
	}
	if got := selector.Select(start.Add(time.Hour + time.Minute)); got != "direct-v6-2" {
		t.Fatalf("Select() after rotation = %q", got)
	}
}

func TestSelectorFallsBackAfterConsecutiveFailures(t *testing.T) {
	start := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	selector := mustSelector(t, start)

	selector.RecordHealth("preferred", false)
	selector.RecordHealth("preferred", true)
	selector.RecordHealth("preferred", false)
	selector.RecordHealth("preferred", false)
	if got := selector.Select(start); got != "preferred" {
		t.Fatalf("Select() before three consecutive failures = %q", got)
	}
	selector.RecordHealth("preferred", false)
	if got := selector.Select(start); got != "fallback" {
		t.Fatalf("Select() after three consecutive failures = %q", got)
	}
}

func TestSelectorReturnsToPreferredAfterConsecutiveSuccesses(t *testing.T) {
	start := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	selector := mustSelector(t, start)
	for range 3 {
		selector.RecordHealth("preferred", false)
	}
	if got := selector.Select(start); got != "fallback" {
		t.Fatalf("Select() after failure = %q", got)
	}

	selector.RecordHealth("preferred", true)
	selector.RecordHealth("preferred", false)
	for range 2 {
		selector.RecordHealth("preferred", true)
	}
	if got := selector.Select(start); got != "fallback" {
		t.Fatalf("Select() before three consecutive recoveries = %q", got)
	}
	selector.RecordHealth("preferred", true)
	if got := selector.Select(start); got != "preferred" {
		t.Fatalf("Select() after three consecutive recoveries = %q", got)
	}
}

func TestSelectorsCanStartAtStaggeredOffsets(t *testing.T) {
	start := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	candidates := []string{"direct-v6-1", "direct-v6-2", "remote-1"}
	want := []string{"direct-v6-1", "direct-v6-2", "remote-1", "direct-v6-1"}
	for index := range want {
		selector, err := NewSelector(SelectorOptions{
			Candidates:        candidates,
			RotationInterval: time.Hour,
			FailureThreshold: 3,
			RecoveryThreshold: 3,
			StartIndex:        index,
			Now:               start,
		})
		if err != nil {
			t.Fatalf("NewSelector(start index %d) error = %v", index, err)
		}
		if got := selector.Select(start); got != want[index] {
			t.Fatalf("selector %d starts at %q, want %q", index, got, want[index])
		}
	}
}

func TestSelectorRejectsUnsafeConfigurationAndUnknownHealthTargets(t *testing.T) {
	start := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	tests := []SelectorOptions{
		{Candidates: nil, RotationInterval: time.Hour, FailureThreshold: 3, RecoveryThreshold: 3, Now: start},
		{Candidates: []string{"same", "same"}, RotationInterval: time.Hour, FailureThreshold: 3, RecoveryThreshold: 3, Now: start},
		{Candidates: []string{"one"}, RotationInterval: 0, FailureThreshold: 3, RecoveryThreshold: 3, Now: start},
		{Candidates: []string{"one"}, RotationInterval: time.Hour, FailureThreshold: 0, RecoveryThreshold: 3, Now: start},
		{Candidates: []string{"one"}, RotationInterval: time.Hour, FailureThreshold: 3, RecoveryThreshold: 0, Now: start},
	}
	for index, options := range tests {
		if _, err := NewSelector(options); err == nil {
			t.Fatalf("NewSelector() accepted unsafe case %d", index)
		}
	}

	selector := mustSelector(t, start)
	if err := selector.RecordHealth("unknown", false); err == nil {
		t.Fatal("RecordHealth() accepted an unknown candidate")
	}
}

func mustSelector(t *testing.T, now time.Time) *Selector {
	t.Helper()
	selector, err := NewSelector(SelectorOptions{
		Candidates:        []string{"preferred", "fallback"},
		RotationInterval: 24 * time.Hour,
		FailureThreshold: 3,
		RecoveryThreshold: 3,
		Now:               now,
	})
	if err != nil {
		t.Fatalf("NewSelector() error = %v", err)
	}
	return selector
}
