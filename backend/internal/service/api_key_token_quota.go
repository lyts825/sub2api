package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrAPIKeyTokenQuotaExhausted = infraerrors.TooManyRequests("API_KEY_TOKEN_QUOTA_EXHAUSTED", "API key token 用量已达上限")

type apiKeyTokenQuotaReader interface {
	GetTokenQuota(ctx context.Context, id int64) (quota, used int64, err error)
}

type tokenQuotaStateReader interface {
	GetTokenQuotaState(ctx context.Context, id int64) (*TokenQuotaState, error)
}

// RefreshTokenQuota bypasses auth caches, including for previously unlimited keys:
// administrators can enable a shared user quota while an existing key is cached.
func (s *APIKeyService) RefreshTokenQuota(ctx context.Context, key *APIKey) error {
	if key == nil {
		return nil
	}
	if repo, ok := s.apiKeyRepo.(tokenQuotaStateReader); ok {
		state, err := repo.GetTokenQuotaState(ctx, key.ID)
		if err != nil {
			return err
		}
		if state == nil {
			return ErrAPIKeyNotFound
		}
		state.Apply(key)
		return nil
	}
	if key.TokenLimits.HasLimits() || key.UserTokenQuota.Quota > 0 {
		return ErrBillingServiceUnavailable
	}
	if key.TokenQuota > 0 {
		quota, used, err := s.GetTokenQuota(ctx, key.ID)
		if err != nil {
			return err
		}
		key.TokenQuota, key.TokenQuotaUsed = quota, used
	}
	return nil
}

func (s *APIKeyService) fillTokenQuotaStates(ctx context.Context, keys []APIKey) error {
	repo, ok := s.apiKeyRepo.(interface {
		GetTokenQuotaStates(context.Context, []int64) (map[int64]*TokenQuotaState, error)
	})
	if !ok || len(keys) == 0 {
		return nil
	}
	ids := make([]int64, len(keys))
	for i := range keys {
		ids[i] = keys[i].ID
	}
	states, err := repo.GetTokenQuotaStates(ctx, ids)
	if err != nil {
		return err
	}
	for i := range keys {
		if state := states[keys[i].ID]; state != nil {
			state.Apply(&keys[i])
		}
	}
	return nil
}

func (s *APIKeyService) fillTokenQuotaResponse(ctx context.Context, key *APIKey) error {
	if _, ok := s.apiKeyRepo.(tokenQuotaStateReader); !ok {
		return nil
	}
	return s.RefreshTokenQuota(ctx, key)
}

// CheckTokenQuota reads current limits and committed usage. Authentication snapshots
// are deliberately not used for enforcement: a cached counter can lag billing.
func (s *APIKeyService) CheckTokenQuota(ctx context.Context, key *APIKey) error {
	if key == nil {
		return nil
	}
	if err := s.RefreshTokenQuota(ctx, key); err != nil {
		return err
	}
	return key.tokenQuotaError()
}

// CheckAPIKeyTokenQuota rechecks committed usage after request queues and for
// async gateway execution, which do not run authentication middleware again.
func (s *BillingCacheService) CheckAPIKeyTokenQuota(ctx context.Context, key *APIKey) error {
	if key == nil {
		return nil
	}
	if s == nil {
		return ErrBillingServiceUnavailable
	}
	if repo, ok := s.apiKeyRateLimitLoader.(tokenQuotaStateReader); ok {
		state, err := repo.GetTokenQuotaState(ctx, key.ID)
		if err != nil {
			return ErrBillingServiceUnavailable.WithCause(err)
		}
		if state == nil {
			return ErrBillingServiceUnavailable
		}
		state.Apply(key)
		markTokenQuotaAccounting(ctx, key)
		return key.tokenQuotaError()
	}
	if !key.HasTokenLimits() {
		return nil
	}
	if key.TokenLimits.HasLimits() || key.UserTokenQuota.Quota > 0 {
		return ErrBillingServiceUnavailable
	}
	repo, ok := s.apiKeyRateLimitLoader.(apiKeyTokenQuotaReader)
	if !ok {
		return ErrBillingServiceUnavailable
	}
	quota, used, err := repo.GetTokenQuota(ctx, key.ID)
	if err != nil {
		return ErrBillingServiceUnavailable.WithCause(err)
	}
	if quota > 0 && used >= quota {
		return ErrAPIKeyTokenQuotaExhausted
	}
	return nil
}

// GetTokenQuota returns the current limit and committed usage without auth caching.
func (s *APIKeyService) GetTokenQuota(ctx context.Context, id int64) (quota, used int64, err error) {
	if repo, ok := s.apiKeyRepo.(apiKeyTokenQuotaReader); ok {
		return repo.GetTokenQuota(ctx, id)
	} else {
		// Alternate repository implementations still read their source of truth.
		current, err := s.apiKeyRepo.GetByID(ctx, id)
		if err != nil {
			return 0, 0, err
		}
		if current == nil {
			return 0, 0, ErrAPIKeyNotFound
		}
		return current.TokenQuota, current.TokenQuotaUsed, nil
	}
}
