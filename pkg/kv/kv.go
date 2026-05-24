package kv

import "fmt"

// Option configures a Store created by New.
type Option func(*options)

type options struct {
	redisURL string
}

// WithRedisURL sets the Redis address for the "redis" driver (host:port).
func WithRedisURL(url string) Option {
	return func(o *options) { o.redisURL = url }
}

// New creates a Store for the given driver ("memory" or "redis").
func New(driver string, opts ...Option) (Store, error) {
	o := &options{redisURL: "localhost:6379"}
	for _, opt := range opts {
		opt(o)
	}
	switch driver {
	case "memory":
		return NewMemoryStore(), nil
	case "redis":
		return NewRedisStore(o.redisURL)
	default:
		return nil, fmt.Errorf("kv: unknown driver %q", driver)
	}
}
