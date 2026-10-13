package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrAPIKeyTokenQuotaExhausted = infraerrors.TooManyRequests("API_KEY_TOKEN_QUOTA_EXHAUSTED", "API key token 用量已达上限")

type apiKeyTokenQuotaReader interface {
	GetTokenQuota(ctx context.Context, id int64) (quota, used int64, err error)
}

// CheckTokenQuota reads committed usage for limited keys. Authentication snapshots
// are deliberately not used for enforcement: a cached counter can lag billing.
func (s *APIKeyService) CheckTokenQuota(ctx context.Context, key *APIKey) error {
	if key == nil || key.TokenQuota <= 0 {
		return nil
	}
	quota, used, err := s.GetTokenQuota(ctx, key.ID)
	if err != nil {
		return err
	}
	if quota > 0 && used >= quota {
		return ErrAPIKeyTokenQuotaExhausted
	}
	return nil
}

// CheckAPIKeyTokenQuota rechecks committed usage after request queues and for
// async gateway execution, which do not run authentication middleware again.
func (s *BillingCacheService) CheckAPIKeyTokenQuota(ctx context.Context, key *APIKey) error {
	if key == nil || key.TokenQuota <= 0 {
		return nil
	}
	if s == nil {
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
