-- Start counting at upgrade. Historical usage logs may already have been purged,
-- so they cannot provide a reliable lifetime total. Zero preserves unlimited keys.
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS token_quota BIGINT NOT NULL DEFAULT 0 CHECK (token_quota >= 0),
    ADD COLUMN IF NOT EXISTS token_quota_used BIGINT NOT NULL DEFAULT 0 CHECK (token_quota_used >= 0);
