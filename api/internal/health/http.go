// Package health provides bounded-timeout readiness probes for the API's
// downstream dependencies. Probes are plain func() error, the shape /readyz
// consults; they only check reachability, not the dependency's protocol.
package health

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// HTTPCheck returns a readiness probe that GETs url with a bounded timeout and
// treats any 2xx response as ready. The error carries the URL and status for
// server-side logs; /readyz must not leak it to clients.
func HTTPCheck(name, url string, timeout time.Duration) func() error {
	// One client per check so its connection pool is reused across polls.
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

// Cached reuses a probe's last result for up to ttl, collapsing bursts of
// polling into one upstream request. ttl <= 0 disables caching.
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
