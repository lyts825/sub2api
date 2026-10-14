-- Separate presentation from billing; NULL keeps the existing displayed rate.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS display_rate_multiplier DECIMAL(10,4) NULL
        CHECK (display_rate_multiplier >= 0 AND display_rate_multiplier <= 999999.9999);

COMMENT ON COLUMN groups.display_rate_multiplier IS
    'User-facing group multiplier only; NULL uses the existing rate. Does not affect billing.';
