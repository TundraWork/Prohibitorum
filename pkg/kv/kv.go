package kv

import "fmt"

// Option configures a Store created by New.
type Option func(*options)

type options struct {
	redisURL      string
	redisUsername string
	redisPassword string
	redisTLS      bool
}

// WithRedisURL sets the Redis address for the "redis" driver (host:port).
func WithRedisURL(url string) Option {
	return func(o *options) { o.redisURL = url }
}

// WithRedisUsername sets the Redis ACL username (Redis 6+). Empty = no username.
func WithRedisUsername(u string) Option {
	return func(o *options) { o.redisUsername = u }
}

// WithRedisPassword sets the Redis AUTH password. Empty = no AUTH.
func WithRedisPassword(p string) Option {
	return func(o *options) { o.redisPassword = p }
}

// WithRedisTLS enables TLS to the Redis server (audit follow-up N9).
func WithRedisTLS(enabled bool) Option {
	return func(o *options) { o.redisTLS = enabled }
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
		return NewRedisStore(RedisConfig{
			Addr:     o.redisURL,
			Username: o.redisUsername,
			Password: o.redisPassword,
			TLS:      o.redisTLS,
		})
	default:
		return nil, fmt.Errorf("kv: unknown driver %q", driver)
	}
}
