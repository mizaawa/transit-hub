-- Per-workspace manual multiplier overrides for admin accounts/channels.
-- The stable target_id is the same platform:workspace:account identifier used by
-- connection_health states and events.
CREATE TABLE IF NOT EXISTS connection_health_account_multipliers (
    user_id         TEXT             NOT NULL,
    admin_account_id TEXT            NOT NULL DEFAULT '',
    target_id       TEXT             NOT NULL,
    multiplier      DOUBLE PRECISION NOT NULL,
    created_at      TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ      NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, admin_account_id, target_id)
);

CREATE INDEX IF NOT EXISTS idx_connection_health_account_multiplier_workspace
    ON connection_health_account_multipliers (user_id, admin_account_id);
