package assistant

import (
	"context"
	"crypto/sha256"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type assistantUsageContextKey struct{}

type assistantUsageIdentity struct {
	account [32]byte
	userID  uint
}

func withAssistantUsage(ctx context.Context, userID uint) context.Context {
	identity := assistantUsageIdentity{
		account: sha256.Sum256([]byte(strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")))),
		userID:  userID,
	}
	return context.WithValue(ctx, assistantUsageContextKey{}, identity)
}

// Only completion HTTP attempts consume this allowance. Catalog queries,
// preference changes and local expense confirmations do not call this gate.
func reserveAssistantCall(ctx context.Context) error {
	identity, ok := ctx.Value(assistantUsageContextKey{}).(assistantUsageIdentity)
	if !ok || identity.userID == 0 {
		return nil
	}
	rpm, daily, err := assistantUsageLimits()
	if err != nil {
		return err
	}
	return assistantCallUsage.reserve(identity, time.Now(), rpm, daily)
}

func assistantUsageLimits() (int, int, error) {
	parse := func(name string, fallback, maximum int) (int, error) {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			return fallback, nil
		}
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > maximum {
			return 0, OpenRouterError{Code: "assistant_configuration_error", StatusCode: http.StatusServiceUnavailable}
		}
		return limit, nil
	}
	rpm, err := parse("OPENROUTER_USER_RPM", 6, 60)
	if err != nil {
		return 0, 0, err
	}
	daily, err := parse("OPENROUTER_USER_DAILY_LIMIT", 20, 1000)
	return rpm, daily, err
}

type assistantUsageEntry struct {
	dayStart int64
	daily    int
	attempts []time.Time
	lastCall time.Time
}

type assistantUsageLimiter struct {
	sync.Mutex
	users      map[assistantUsageIdentity]*assistantUsageEntry
	lastPruned time.Time
}

var assistantCallUsage assistantUsageLimiter

func (limiter *assistantUsageLimiter) reserve(identity assistantUsageIdentity, now time.Time, rpm, daily int) error {
	limiter.Lock()
	defer limiter.Unlock()
	if limiter.users == nil {
		limiter.users = make(map[assistantUsageIdentity]*assistantUsageEntry)
	}
	utc := now.UTC()
	midnight := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	minuteAgo := now.Add(-time.Minute)
	if limiter.lastPruned.IsZero() || now.Sub(limiter.lastPruned) >= time.Minute {
		for id, entry := range limiter.users {
			// Keep the previous day's last minute for a true rolling minute limit
			// across midnight. Every other expired daily entry can be discarded.
			if entry.dayStart != midnight.Unix() && !entry.lastCall.After(minuteAgo) {
				delete(limiter.users, id)
			}
		}
		limiter.lastPruned = now
	}
	entry := limiter.users[identity]
	if entry == nil {
		entry = &assistantUsageEntry{dayStart: midnight.Unix()}
		limiter.users[identity] = entry
	}
	if entry.dayStart != midnight.Unix() {
		entry.dayStart = midnight.Unix()
		entry.daily = 0
	}
	active := entry.attempts[:0]
	for _, attempt := range entry.attempts {
		if attempt.After(minuteAgo) {
			active = append(active, attempt)
		}
	}
	entry.attempts = active
	if entry.daily >= daily {
		return assistantUserLimitError(midnight.AddDate(0, 0, 1).Sub(now))
	}
	if len(entry.attempts) >= rpm {
		return assistantUserLimitError(entry.attempts[0].Add(time.Minute).Sub(now))
	}
	entry.daily++
	entry.attempts = append(entry.attempts, now)
	entry.lastCall = now
	return nil
}

func assistantUserLimitError(remaining time.Duration) error {
	seconds := int((remaining + time.Second - 1) / time.Second)
	return OpenRouterError{
		Code: "assistant_user_limit", StatusCode: http.StatusTooManyRequests,
		RetryAfterSeconds: min(max(seconds, 1), 86400),
	}
}
