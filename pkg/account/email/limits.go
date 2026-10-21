// limits.go — send-rate limits for verification-code mails, built on
// ulule/limiter with a memory or go-redis store chosen from the KV driver.
//
// No key ever contains an IP: the card's binding constraint. Keys are the
// address id (cooldown), the account id (hourly), the lowercased address
// (cross-account hourly), and the admin account id (test-mail per minute).
package email

import (
	"context"
	"fmt"
	"time"

	limiter "github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/store/memory"
	limiterredis "github.com/ulule/limiter/v3/drivers/store/redis"

	"prohibitorum/pkg/kv"
)

// SendLimits carries the four limiters. Zero value is not usable; build with
// NewSendLimits.
type SendLimits struct {
	cooldown *limiter.Limiter // 1 send per 60s per address
	account  *limiter.Limiter // 10 sends per hour per account
	address  *limiter.Limiter // 5 sends per hour per lower(address), cross-account
	testMail *limiter.Limiter // 5 test mails per minute per admin
}

// NewSendLimits picks the store from the KV driver: a shared Redis when KV is
// redis (so replicas count together), the in-process store otherwise.
func NewSendLimits(kvStore kv.Store) (*SendLimits, error) {
	store, err := limiterStoreFor(kvStore)
	if err != nil {
		return nil, err
	}
	cooldown, err := rateLimiter(store, "1-M")
	if err != nil {
		return nil, err
	}
	account, err := rateLimiter(store, "10-H")
	if err != nil {
		return nil, err
	}
	address, err := rateLimiter(store, "5-H")
	if err != nil {
		return nil, err
	}
	testMail, err := rateLimiter(store, "5-M")
	if err != nil {
		return nil, err
	}
	return &SendLimits{cooldown: cooldown, account: account, address: address, testMail: testMail}, nil
}

func rateLimiter(store limiter.Store, formatted string) (*limiter.Limiter, error) {
	rate, err := limiter.NewRateFromFormatted(formatted)
	if err != nil {
		return nil, fmt.Errorf("email: parse rate %q: %w", formatted, err)
	}
	return limiter.New(store, rate), nil
}

func limiterStoreFor(kvStore kv.Store) (limiter.Store, error) {
	if rs, ok := kvStore.(*kv.RedisStore); ok {
		store, err := limiterredis.NewStore(rs.RedisClient())
		if err != nil {
			return nil, fmt.Errorf("email: redis limiter store: %w", err)
		}
		return store, nil
	}
	return memory.NewStore(), nil
}

const (
	cooldownKeyPrefix = "email-code:cooldown:"
	accountKeyPrefix  = "email-code:account:"
	addressKeyPrefix  = "email-code:address:"
	testMailKeyPrefix = "email-test:admin:"
)

// AllowCodeSend checks and consumes all three code-send buckets. On refusal
// it returns the duration after which the tightest bucket resets.
func (l *SendLimits) AllowCodeSend(ctx context.Context, accountID int32, emailID int64, addressLower string) (time.Duration, bool) {
	type bucket struct {
		lim  *limiter.Limiter
		key  string
		name string
	}
	buckets := []bucket{
		{l.cooldown, fmt.Sprintf("%s%d", cooldownKeyPrefix, emailID), "cooldown"},
		{l.account, fmt.Sprintf("%s%d", accountKeyPrefix, accountID), "account"},
		{l.address, addressKeyPrefix + addressLower, "address"},
	}
	// Check everything first, then consume, so a refusal doesn't burn the
	// buckets that still had room.
	for _, b := range buckets {
		c, err := b.lim.Peek(ctx, b.key)
		if err == nil && c.Reached {
			return time.Until(time.Unix(c.Reset, 0)), false
		}
	}
	for _, b := range buckets {
		c, err := b.lim.Get(ctx, b.key)
		if err != nil {
			// Store failure: fail open. Mail cost is bounded by SMTP and
			// the cooldown key anyway; refusing verification over a
			// limiter hiccup is worse.
			continue
		}
		if c.Reached {
			return time.Until(time.Unix(c.Reset, 0)), false
		}
	}
	return 0, true
}

// CooldownRemaining reports how long until the 60s cooldown for the address
// resets, for the "resend in Ns" UI copy. 0 when no cooldown is active.
func (l *SendLimits) CooldownRemaining(ctx context.Context, emailID int64) time.Duration {
	c, err := l.cooldown.Peek(ctx, fmt.Sprintf("%s%d", cooldownKeyPrefix, emailID))
	if err != nil || !c.Reached {
		return 0
	}
	return time.Until(time.Unix(c.Reset, 0))
}

// AllowTestMail checks and consumes the per-admin test-mail bucket.
func (l *SendLimits) AllowTestMail(ctx context.Context, adminID int32) (time.Duration, bool) {
	key := fmt.Sprintf("%s%d", testMailKeyPrefix, adminID)
	c, err := l.testMail.Get(ctx, key)
	if err != nil {
		return 0, true
	}
	if c.Reached {
		return time.Until(time.Unix(c.Reset, 0)), false
	}
	return 0, true
}
