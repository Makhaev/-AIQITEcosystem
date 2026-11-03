CREATE TABLE password_reset_tokens (
    user_id bigint PRIMARY KEY REFERENCES users(id),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX password_reset_tokens_expiry ON password_reset_tokens(expires_at);

CREATE TABLE password_reset_rate_limits (
    key_hash bytea PRIMARY KEY CHECK (octet_length(key_hash) = 32),
    attempts integer NOT NULL,
    window_start timestamptz NOT NULL
);

-- Administrative resets, ordinary password changes and blocking invalidate links.
-- All callers already lock the user first; reset operations keep that lock order.
CREATE FUNCTION invalidate_password_reset_tokens() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.password_hash IS DISTINCT FROM OLD.password_hash
       OR NEW.blocked IS DISTINCT FROM OLD.blocked THEN
        DELETE FROM password_reset_tokens WHERE user_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER users_invalidate_password_reset
AFTER UPDATE OF password_hash, blocked ON users
FOR EACH ROW EXECUTE FUNCTION invalidate_password_reset_tokens();
