// Package health provides lightweight, bounded-timeout readiness probes for the
// API's downstream dependencies (Meilisearch, Kroki, Garage).
//
// Each probe is a plain func() error — the same shape the server's /readyz
// consults (see internal/server) and that db.ReadinessCheck already produces for
// Postgres. Probes are deliberately dependency-free HTTP GETs: readiness only
// needs to know a dependency is *reachable*, not to speak its protocol, so the
// real Meilisearch/S3 clients stay in their feature tickets.
package health

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// HTTPCheck returns a readiness probe that GETs url with a bounded timeout and
// treats any 2xx response as ready. name identifies the dependency in the
// returned error.
//
// The error is intentionally descriptive (it carries the URL and status) because
// it is meant for server-side logs; the /readyz handler is responsible for not
// leaking it to clients (06-security.md: never return raw connection details).
func HTTPCheck(name, url string, timeout time.Duration) func() error {
	// One client per check so the transport (and its connection pool) is reused
	// across polls rather than rebuilt on every probe.
	client := &http.Client{Timeout: timeout}
	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("%s: build request for %s: %w", name, url, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("%s: GET %s: %w", name, url, err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("%s: GET %s returned %d", name, url, resp.StatusCode)
		}
		return nil
	}
}

// Cached wraps a probe so its most recent result is reused for up to ttl,
// collapsing bursts of orchestrator polling into a single upstream request
// (01-architecture.md §6: /readyz is polled frequently). A ttl <= 0 disables
// caching and calls through every time.
func Cached(ttl time.Duration, check func() error) func() error {
	if ttl <= 0 {
		return check
	}
	var (
		mu       sync.Mutex
		lastRun  time.Time
		lastErr  error
		hasValue bool
	)
	return func() error {
		mu.Lock()
		defer mu.Unlock()
		if hasValue && time.Since(lastRun) < ttl {
			return lastErr
		}
		lastErr = check()
		lastRun = time.Now()
		hasValue = true
		return lastErr
	}
}
