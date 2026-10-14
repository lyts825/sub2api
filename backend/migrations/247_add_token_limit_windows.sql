-- New counters start at upgrade; historical usage may already have been purged.
CREATE TABLE IF NOT EXISTS api_key_token_windows (
    api_key_id BIGINT PRIMARY KEY REFERENCES api_keys(id) ON DELETE CASCADE,
    daily_limit BIGINT NOT NULL DEFAULT 0 CHECK (daily_limit >= 0),
    weekly_limit BIGINT NOT NULL DEFAULT 0 CHECK (weekly_limit >= 0),
    monthly_limit BIGINT NOT NULL DEFAULT 0 CHECK (monthly_limit >= 0),
    tpm_limit BIGINT NOT NULL DEFAULT 0 CHECK (tpm_limit >= 0),
    daily_used BIGINT NOT NULL DEFAULT 0 CHECK (daily_used >= 0),
    weekly_used BIGINT NOT NULL DEFAULT 0 CHECK (weekly_used >= 0),
    monthly_used BIGINT NOT NULL DEFAULT 0 CHECK (monthly_used >= 0),
    tpm_used BIGINT NOT NULL DEFAULT 0 CHECK (tpm_used >= 0),
    daily_start TIMESTAMPTZ,
    weekly_start TIMESTAMPTZ,
    monthly_start TIMESTAMPTZ,
    tpm_start TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS user_token_quotas (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    quota BIGINT NOT NULL DEFAULT 0 CHECK (quota >= 0),
    used BIGINT NOT NULL DEFAULT 0 CHECK (used >= 0)
);
