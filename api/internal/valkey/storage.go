// Package valkey adapts a Valkey (Redis-protocol) client to fiber.Storage so the
// signup/login rate limiter can share its counters across restarts/instances.
package valkey

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// keyPrefix namespaces every key this storage writes, so Reset never touches
// anything else in the database.
const keyPrefix = "lookupkb:ratelimit:"

// opTimeout bounds each storage call so a stalled Valkey cannot hang a request.
const opTimeout = 2 * time.Second

// Storage implements fiber.Storage on top of a go-redis client.
type Storage struct {
	client *redis.Client
}

// New connects to the redis:// URL and verifies the connection with a PING.
func New(ctx context.Context, url string) (*Storage, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse valkey url: %w", err)
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping valkey: %w", err)
	}
	return &Storage{client: client}, nil
}

// Get returns the value for key, or nil when it does not exist.
func (s *Storage) Get(key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	val, err := s.client.Get(ctx, keyPrefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	return val, err
}

// Set stores val under key; exp == 0 means no expiry.
func (s *Storage) Set(key string, val []byte, exp time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	return s.client.Set(ctx, keyPrefix+key, val, exp).Err()
}

// Delete removes key.
func (s *Storage) Delete(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	return s.client.Del(ctx, keyPrefix+key).Err()
}

// Reset removes every key under this storage's prefix (never FLUSHDB).
func (s *Storage) Reset() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*opTimeout)
	defer cancel()
	iter := s.client.Scan(ctx, 0, keyPrefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		if err := s.client.Del(ctx, iter.Val()).Err(); err != nil {
			return err
		}
	}
	return iter.Err()
}

// Close releases the client's connections.
func (s *Storage) Close() error {
	return s.client.Close()
}

// Ping reports whether Valkey is reachable (used as a /readyz check).
func (s *Storage) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	return s.client.Ping(ctx).Err()
}
