CREATE TABLE users (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email text NOT NULL CHECK (email = lower(btrim(email))),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    password_hash text NOT NULL,
    blocked boolean NOT NULL DEFAULT false,
    must_change_password boolean NOT NULL DEFAULT true,
    failed_attempts integer NOT NULL DEFAULT 0,
    failure_window_start timestamptz,
    locked_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_unique ON users (lower(email));

CREATE TABLE user_roles (
    user_id bigint NOT NULL REFERENCES users(id),
    role text NOT NULL CHECK (role IN ('admin', 'leader', 'teacher', 'curator')),
    PRIMARY KEY (user_id, role)
);

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    csrf_hash bytea NOT NULL CHECK (octet_length(csrf_hash) = 32),
    user_id bigint NOT NULL REFERENCES users(id),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_id ON sessions(user_id);
CREATE INDEX sessions_expiry ON sessions(expires_at);

CREATE TABLE audit_logs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    actor_id bigint REFERENCES users(id),
    action text NOT NULL,
    object_id bigint REFERENCES users(id),
    request_id text NOT NULL DEFAULT ''
);

-- Only aggregate IP hashes are stored, never raw IP addresses.
CREATE TABLE login_rate_limits (
    key_hash bytea PRIMARY KEY,
    attempts integer NOT NULL,
    window_start timestamptz NOT NULL
);
