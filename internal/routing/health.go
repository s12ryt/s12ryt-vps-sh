package routing

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
)

type Probe interface {
	Probe(ctx context.Context, candidate, url string) error
}

type HealthMonitorOptions struct {
	Selector   *Selector
	Candidates []string
	URL        string
	Interval   time.Duration
	Timeout    time.Duration
	Probe      Probe
	newTicker  healthTickerFactory
}

type HealthMonitor struct {
	selector   *Selector
	candidates []string
	url        string
	interval   time.Duration
	timeout    time.Duration
	probe      Probe
	newTicker  healthTickerFactory
}

type healthTicker interface {
	C() <-chan time.Time
	Stop()
}

type healthTickerFactory func(time.Duration) healthTicker

type timeTicker struct {
	*time.Ticker
}

func (ticker timeTicker) C() <-chan time.Time { return ticker.Ticker.C }

func NewHealthMonitor(options HealthMonitorOptions) (*HealthMonitor, error) {
	if options.Selector == nil {
		return nil, errors.New("health monitor selector is required")
	}
	if len(options.Candidates) == 0 {
		return nil, errors.New("health monitor candidates are required")
	}
	candidates := append([]string(nil), options.Candidates...)
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			return nil, errors.New("health monitor candidate cannot be empty")
		}
		if _, duplicate := seen[candidate]; duplicate {
			return nil, fmt.Errorf("duplicate health monitor candidate %q", candidate)
		}
		seen[candidate] = struct{}{}
	}
	parsedURL, err := url.Parse(options.URL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		return nil, errors.New("health monitor URL must be absolute HTTPS")
	}
	if options.Interval <= 0 {
		return nil, errors.New("health monitor interval must be positive")
	}
	if options.Timeout <= 0 {
		return nil, errors.New("health monitor timeout must be positive")
	}
	if options.Probe == nil {
		return nil, errors.New("health monitor probe is required")
	}
	newTicker := options.newTicker
	if newTicker == nil {
		newTicker = func(interval time.Duration) healthTicker {
			return timeTicker{Ticker: time.NewTicker(interval)}
		}
	}

	return &HealthMonitor{
		selector:   options.Selector,
		candidates: candidates,
		url:        options.URL,
		interval:   options.Interval,
		timeout:    options.Timeout,
		probe:      options.Probe,
		newTicker:  newTicker,
	}, nil
}

func (monitor *HealthMonitor) CheckOnce(ctx context.Context) error {
	for _, candidate := range monitor.candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		probeContext, cancel := context.WithTimeout(ctx, monitor.timeout)
		err := monitor.probe.Probe(probeContext, candidate, monitor.url)
		cancel()
		if recordErr := monitor.selector.RecordHealth(candidate, err == nil); recordErr != nil {
			return recordErr
		}
	}
	return nil
}

func (monitor *HealthMonitor) Run(ctx context.Context) error {
	if err := monitor.CheckOnce(ctx); err != nil {
		return err
	}
	ticker := monitor.newTicker(monitor.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C():
			if err := monitor.CheckOnce(ctx); err != nil {
				return err
			}
		}
	}
}
