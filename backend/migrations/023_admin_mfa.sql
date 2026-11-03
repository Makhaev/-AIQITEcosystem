ALTER TABLE sessions ADD COLUMN mfa_verified boolean NOT NULL DEFAULT false;
ALTER TABLE sessions ADD COLUMN mfa_pending_secret bytea;
ALTER TABLE sessions ADD COLUMN mfa_setup_expires timestamptz;

CREATE TABLE user_mfa (
    user_id bigint PRIMARY KEY REFERENCES users(id),
    secret_cipher bytea NOT NULL,
    last_step bigint NOT NULL DEFAULT -1,
    enabled_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE mfa_recovery_codes (
    user_id bigint NOT NULL REFERENCES users(id),
    code_hash bytea NOT NULL CHECK (octet_length(code_hash)=32),
    PRIMARY KEY(user_id,code_hash)
);
CREATE TABLE mfa_rate_limits (
    key_hash bytea PRIMARY KEY CHECK (octet_length(key_hash)=32),
    attempts integer NOT NULL,
    window_start timestamptz NOT NULL
);
-- An existing password-only session must never become an administrative session.
DELETE FROM sessions WHERE user_id IN (SELECT user_id FROM user_roles WHERE role='admin');

-- Only owner-operated CLI resets populate these fields. No factor secrets belong here.
ALTER TABLE audit_logs ADD COLUMN mfa_reset_operator text;
ALTER TABLE audit_logs ADD COLUMN mfa_reset_reason text;
ALTER TABLE audit_logs ADD CONSTRAINT mfa_reset_audit_details CHECK (
    (mfa_reset_operator IS NULL AND mfa_reset_reason IS NULL)
    OR (action='auth.mfa_reset' AND length(btrim(mfa_reset_operator)) BETWEEN 1 AND 200
        AND length(btrim(mfa_reset_reason)) BETWEEN 1 AND 1000
        AND mfa_reset_operator IS NOT NULL AND mfa_reset_reason IS NOT NULL)
);
