package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func (r *apiKeyRepository) tokenLimitsExecutor(ctx context.Context) sqlExecutor {
	if r.client != nil {
		return clientFromContext(ctx, r.client)
	}
	return r.sql
}

// Keep key mutations and their limits in the caller's transaction when present.
func (r *apiKeyRepository) withTokenLimitsTransaction(ctx context.Context, fn func(context.Context) error) error {
	if dbent.TxFromContext(ctx) != nil {
		return fn(ctx)
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return fn(ctx)
	}
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(dbent.NewTxContext(ctx, tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *apiKeyRepository) SetTokenLimits(ctx context.Context, id int64, limits service.TokenLimitConfig) error {
	result, err := r.tokenLimitsExecutor(ctx).ExecContext(ctx, `
		INSERT INTO api_key_token_windows (api_key_id, daily_limit, weekly_limit, monthly_limit, tpm_limit)
		SELECT id, $2, $3, $4, $5 FROM api_keys WHERE id = $1 AND deleted_at IS NULL
		ON CONFLICT (api_key_id) DO UPDATE SET
			daily_limit = EXCLUDED.daily_limit, weekly_limit = EXCLUDED.weekly_limit,
			monthly_limit = EXCLUDED.monthly_limit, tpm_limit = EXCLUDED.tpm_limit
	`, id, limits.Daily, limits.Weekly, limits.Monthly, limits.TPM)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err == nil && affected == 0 {
		return service.ErrAPIKeyNotFound
	}
	return err
}

func (r *apiKeyRepository) GetTokenQuotaState(ctx context.Context, id int64) (*service.TokenQuotaState, error) {
	states, err := r.GetTokenQuotaStates(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	state, ok := states[id]
	if !ok {
		return nil, service.ErrAPIKeyNotFound
	}
	return state, nil
}

// Batch all limits and effective window usage to avoid per-key list queries.
func (r *apiKeyRepository) GetTokenQuotaStates(ctx context.Context, ids []int64) (map[int64]*service.TokenQuotaState, error) {
	states := make(map[int64]*service.TokenQuotaState, len(ids))
	if len(ids) == 0 {
		return states, nil
	}
	rows, err := r.tokenLimitsExecutor(ctx).QueryContext(ctx, `
		SELECT k.id, k.token_quota, k.token_quota_used,
			COALESCE(w.daily_limit, 0), COALESCE(w.weekly_limit, 0), COALESCE(w.monthly_limit, 0), COALESCE(w.tpm_limit, 0),
			COALESCE(w.daily_used, 0), COALESCE(w.weekly_used, 0), COALESCE(w.monthly_used, 0), COALESCE(w.tpm_used, 0),
			w.daily_start, w.weekly_start, w.monthly_start, w.tpm_start,
			COALESCE(uq.quota, 0), COALESCE(uq.used, 0)
		FROM api_keys k
		LEFT JOIN api_key_token_windows w ON w.api_key_id = k.id
		LEFT JOIN user_token_quotas uq ON uq.user_id = k.user_id
		WHERE k.id = ANY($1::bigint[]) AND k.deleted_at IS NULL
	`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bounds := service.CurrentTokenWindowBounds(timezone.Now())
	for rows.Next() {
		var id int64
		var state service.TokenQuotaState
		var dailyStart, weeklyStart, monthlyStart, tpmStart sql.NullTime
		if err := rows.Scan(&id, &state.Quota, &state.Used,
			&state.Limits.Daily, &state.Limits.Weekly, &state.Limits.Monthly, &state.Limits.TPM,
			&state.Usage.Daily.Used, &state.Usage.Weekly.Used, &state.Usage.Monthly.Used, &state.Usage.TPM.Used,
			&dailyStart, &weeklyStart, &monthlyStart, &tpmStart, &state.UserQuota.Quota, &state.UserQuota.Used); err != nil {
			return nil, err
		}
		applyTokenWindowState(&state.Usage.Daily, state.Limits.Daily, dailyStart, bounds["daily"])
		applyTokenWindowState(&state.Usage.Weekly, state.Limits.Weekly, weeklyStart, bounds["weekly"])
		applyTokenWindowState(&state.Usage.Monthly, state.Limits.Monthly, monthlyStart, bounds["monthly"])
		applyTokenWindowState(&state.Usage.TPM, state.Limits.TPM, tpmStart, bounds["tpm"])
		states[id] = &state
	}
	return states, rows.Err()
}

func applyTokenWindowState(window *service.TokenWindow, limit int64, start sql.NullTime, bounds service.TokenWindowBounds) {
	window.Limit, window.ResetsAt = limit, bounds.End
	if !start.Valid || start.Time.Before(bounds.Start) {
		window.Used = 0
	}
}

func (r *apiKeyRepository) GetUserTokenQuota(ctx context.Context, userID int64) (*service.UserTokenQuota, error) {
	var quota service.UserTokenQuota
	err := scanSingleRow(ctx, r.tokenLimitsExecutor(ctx), `
		SELECT COALESCE(q.quota, 0), COALESCE(q.used, 0)
		FROM users u LEFT JOIN user_token_quotas q ON q.user_id = u.id
		WHERE u.id = $1 AND u.deleted_at IS NULL
	`, []any{userID}, &quota.Quota, &quota.Used)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUserNotFound
	}
	return &quota, err
}

func (r *apiKeyRepository) UpdateUserTokenQuota(ctx context.Context, userID int64, quota *int64, resetUsed bool) (*service.UserTokenQuota, error) {
	var result service.UserTokenQuota
	err := scanSingleRow(ctx, r.tokenLimitsExecutor(ctx), `
		INSERT INTO user_token_quotas (user_id, quota, used)
		SELECT id, COALESCE($2::bigint, 0), 0 FROM users WHERE id = $1 AND deleted_at IS NULL
		ON CONFLICT (user_id) DO UPDATE SET
			quota = COALESCE($2::bigint, user_token_quotas.quota),
			used = CASE WHEN $3 THEN 0 ELSE user_token_quotas.used END
		RETURNING quota, used
	`, []any{userID, quota, resetUsed}, &result.Quota, &result.Used)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUserNotFound
	}
	return &result, err
}

// All token counters share the request's existing billing dedup transaction.
func incrementUsageBillingTokens(ctx context.Context, tx *sql.Tx, userID, keyID, tokens int64, now time.Time) error {
	// User totals survive key deletion, including requests already in flight.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO user_token_quotas (user_id, used)
		SELECT id, $2 FROM users WHERE id = $1 AND deleted_at IS NULL
		ON CONFLICT (user_id) DO UPDATE SET used = user_token_quotas.used + EXCLUDED.used
	`, userID, tokens); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET token_quota_used = token_quota_used + $1, updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`, tokens, keyID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return err
	}
	bounds := service.CurrentTokenWindowBounds(now)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO api_key_token_windows (
			api_key_id, daily_used, weekly_used, monthly_used, tpm_used,
			daily_start, weekly_start, monthly_start, tpm_start
		) VALUES ($1, $2, $2, $2, $2, $3, $4, $5, $6)
		ON CONFLICT (api_key_id) DO UPDATE SET
			daily_used = CASE WHEN api_key_token_windows.daily_start IS NULL OR api_key_token_windows.daily_start < EXCLUDED.daily_start THEN EXCLUDED.daily_used ELSE api_key_token_windows.daily_used + EXCLUDED.daily_used END,
			weekly_used = CASE WHEN api_key_token_windows.weekly_start IS NULL OR api_key_token_windows.weekly_start < EXCLUDED.weekly_start THEN EXCLUDED.weekly_used ELSE api_key_token_windows.weekly_used + EXCLUDED.weekly_used END,
			monthly_used = CASE WHEN api_key_token_windows.monthly_start IS NULL OR api_key_token_windows.monthly_start < EXCLUDED.monthly_start THEN EXCLUDED.monthly_used ELSE api_key_token_windows.monthly_used + EXCLUDED.monthly_used END,
			tpm_used = CASE WHEN api_key_token_windows.tpm_start IS NULL OR api_key_token_windows.tpm_start < EXCLUDED.tpm_start THEN EXCLUDED.tpm_used ELSE api_key_token_windows.tpm_used + EXCLUDED.tpm_used END,
			daily_start = GREATEST(api_key_token_windows.daily_start, EXCLUDED.daily_start),
			weekly_start = GREATEST(api_key_token_windows.weekly_start, EXCLUDED.weekly_start),
			monthly_start = GREATEST(api_key_token_windows.monthly_start, EXCLUDED.monthly_start),
			tpm_start = GREATEST(api_key_token_windows.tpm_start, EXCLUDED.tpm_start)
	`, keyID, tokens, bounds["daily"].Start, bounds["weekly"].Start, bounds["monthly"].Start, bounds["tpm"].Start)
	return err
}
