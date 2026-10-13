package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *apiKeyRepository) GetTokenQuota(ctx context.Context, id int64) (quota, used int64, err error) {
	err = scanSingleRow(ctx, r.sql, `
		SELECT token_quota, token_quota_used
		FROM api_keys WHERE id = $1 AND deleted_at IS NULL
	`, []any{id}, &quota, &used)
	if errors.Is(err, sql.ErrNoRows) {
		err = service.ErrAPIKeyNotFound
	}
	return
}
