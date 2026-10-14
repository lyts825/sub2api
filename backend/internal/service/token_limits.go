package service

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// TokenLimitConfig is replaced as a whole when supplied; zero means unlimited.
type TokenLimitConfig struct {
	Daily   int64 `json:"daily"`
	Weekly  int64 `json:"weekly"`
	Monthly int64 `json:"monthly"`
	TPM     int64 `json:"tpm"`
}

func (l TokenLimitConfig) HasLimits() bool {
	return l.Daily > 0 || l.Weekly > 0 || l.Monthly > 0 || l.TPM > 0
}

func (l TokenLimitConfig) Validate() error {
	if l.Daily < 0 || l.Weekly < 0 || l.Monthly < 0 || l.TPM < 0 {
		return infraerrors.BadRequest("API_KEY_LIMIT_INVALID", "token limits must be non-negative integers")
	}
	return nil
}

type TokenWindow struct {
	Limit    int64     `json:"limit"`
	Used     int64     `json:"used"`
	ResetsAt time.Time `json:"resets_at"`
}

type TokenWindowUsage struct {
	Daily   TokenWindow `json:"daily"`
	Weekly  TokenWindow `json:"weekly"`
	Monthly TokenWindow `json:"monthly"`
	TPM     TokenWindow `json:"tpm"`
}

type UserTokenQuota struct {
	Quota int64 `json:"quota"`
	Used  int64 `json:"used"`
}

type TokenQuotaState struct {
	Quota     int64
	Used      int64
	Limits    TokenLimitConfig
	Usage     TokenWindowUsage
	UserQuota UserTokenQuota
}

func (s *TokenQuotaState) Apply(key *APIKey) {
	key.TokenQuota, key.TokenQuotaUsed = s.Quota, s.Used
	key.TokenLimits, key.TokenUsage, key.UserTokenQuota = s.Limits, s.Usage, s.UserQuota
}

func (k *APIKey) HasTokenLimits() bool {
	return k != nil && (k.TokenQuota > 0 || k.TokenLimits.HasLimits() || k.UserTokenQuota.Quota > 0)
}

type tokenQuotaAccountingKey struct{}

// TrackTokenQuotaAccounting lets post-queue checks upgrade the accounting policy
// when a quota was enabled after authentication, without sharing mutable keys.
func TrackTokenQuotaAccounting(ctx context.Context, key *APIKey) context.Context {
	flag := &atomic.Bool{}
	flag.Store(key.HasTokenLimits())
	return context.WithValue(ctx, tokenQuotaAccountingKey{}, flag)
}

func markTokenQuotaAccounting(ctx context.Context, key *APIKey) {
	if key.HasTokenLimits() {
		if flag, ok := ctx.Value(tokenQuotaAccountingKey{}).(*atomic.Bool); ok {
			flag.Store(true)
		}
	}
}

func TokenQuotaAccountingRequired(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if ctx.Value(ctxkey.APIKeyTokenQuotaEnabled) == true {
		return true
	}
	flag, ok := ctx.Value(tokenQuotaAccountingKey{}).(*atomic.Bool)
	return ok && flag.Load()
}

type TokenWindowBounds struct{ Start, End time.Time }

// Calendar windows use the configured service timezone, including DST changes.
// The TPM window is a fixed minute; this is not a rolling 60-second reservation.
func CurrentTokenWindowBounds(now time.Time) map[string]TokenWindowBounds {
	day := timezone.StartOfDay(now)
	week := timezone.StartOfWeek(now)
	month := timezone.StartOfMonth(now)
	minute := now.Truncate(time.Minute)
	return map[string]TokenWindowBounds{
		"daily":   {day, day.AddDate(0, 0, 1)},
		"weekly":  {week, week.AddDate(0, 0, 7)},
		"monthly": {month, month.AddDate(0, 1, 0)},
		"tpm":     {minute, minute.Add(time.Minute)},
	}
}

var (
	ErrAPIKeyTokenDailyExhausted   = infraerrors.TooManyRequests("API_KEY_TOKEN_DAILY_EXHAUSTED", "API key 每日 token 额度已用完")
	ErrAPIKeyTokenWeeklyExhausted  = infraerrors.TooManyRequests("API_KEY_TOKEN_WEEKLY_EXHAUSTED", "API key 每周 token 额度已用完")
	ErrAPIKeyTokenMonthlyExhausted = infraerrors.TooManyRequests("API_KEY_TOKEN_MONTHLY_EXHAUSTED", "API key 每月 token 额度已用完")
	ErrAPIKeyTokenTPMExceeded      = infraerrors.TooManyRequests("API_KEY_TOKEN_TPM_EXCEEDED", "API key 每分钟 token 限额已用完")
	ErrUserTokenQuotaExhausted     = infraerrors.TooManyRequests("USER_TOKEN_QUOTA_EXHAUSTED", "用户共享 token 总额度已用完")
)

func (k *APIKey) tokenQuotaError() error {
	if k.TokenQuota > 0 && k.TokenQuotaUsed >= k.TokenQuota {
		return ErrAPIKeyTokenQuotaExhausted
	}
	if k.UserTokenQuota.Quota > 0 && k.UserTokenQuota.Used >= k.UserTokenQuota.Quota {
		return ErrUserTokenQuotaExhausted
	}
	for _, limit := range []struct {
		configured int64
		window     TokenWindow
		err        *infraerrors.ApplicationError
	}{
		{k.TokenLimits.Daily, k.TokenUsage.Daily, ErrAPIKeyTokenDailyExhausted},
		{k.TokenLimits.Weekly, k.TokenUsage.Weekly, ErrAPIKeyTokenWeeklyExhausted},
		{k.TokenLimits.Monthly, k.TokenUsage.Monthly, ErrAPIKeyTokenMonthlyExhausted},
		{k.TokenLimits.TPM, k.TokenUsage.TPM, ErrAPIKeyTokenTPMExceeded},
	} {
		if limit.configured > 0 && limit.window.Used >= limit.configured {
			return limit.err.WithMetadata(map[string]string{"window_resets_at": limit.window.ResetsAt.UTC().Format(time.RFC3339)})
		}
	}
	return nil
}

func IsTokenQuotaExceeded(err error) bool {
	for _, target := range []error{ErrAPIKeyTokenQuotaExhausted, ErrUserTokenQuotaExhausted, ErrAPIKeyTokenDailyExhausted, ErrAPIKeyTokenWeeklyExhausted, ErrAPIKeyTokenMonthlyExhausted, ErrAPIKeyTokenTPMExceeded} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func TokenQuotaRetryAfter(err error) int {
	var e *infraerrors.ApplicationError
	if !errors.As(err, &e) {
		return 0
	}
	reset, parseErr := time.Parse(time.RFC3339, e.Metadata["window_resets_at"])
	if parseErr != nil {
		return 0
	}
	return max(1, int(math.Ceil(time.Until(reset).Seconds())))
}
