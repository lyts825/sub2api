package service

import (
	"context"
	"net/http"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// Keep token quota management separate from the general user update path so an
// ordinary profile update cannot change an administrator's shared allowance.
type userTokenQuotaRepository interface {
	GetUserTokenQuota(ctx context.Context, userID int64) (*UserTokenQuota, error)
	UpdateUserTokenQuota(ctx context.Context, userID int64, quota *int64, resetUsed bool) (*UserTokenQuota, error)
}

func (s *adminServiceImpl) GetUserTokenQuota(ctx context.Context, userID int64) (*UserTokenQuota, error) {
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_INPUT", "user_id must be greater than 0")
	}
	repo, ok := s.apiKeyRepo.(userTokenQuotaRepository)
	if !ok {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "TOKEN_QUOTA_UNAVAILABLE", "token quota service not available")
	}
	return repo.GetUserTokenQuota(ctx, userID)
}

func (s *adminServiceImpl) UpdateUserTokenQuota(ctx context.Context, userID int64, quota *int64, resetUsed bool) (*UserTokenQuota, error) {
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_INPUT", "user_id must be greater than 0")
	}
	if quota != nil && *quota < 0 {
		return nil, infraerrors.BadRequest("INVALID_INPUT", "quota must be a non-negative integer")
	}
	repo, ok := s.apiKeyRepo.(userTokenQuotaRepository)
	if !ok {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "TOKEN_QUOTA_UNAVAILABLE", "token quota service not available")
	}
	return repo.UpdateUserTokenQuota(ctx, userID, quota, resetUsed)
}
