ALTER TABLE user_roles DROP CONSTRAINT user_roles_role_check;
ALTER TABLE user_roles ADD CONSTRAINT user_roles_role_check
    CHECK (role IN ('admin','leader','teacher','curator','methodologist','education_leader','parent','student'));

CREATE TABLE invitations (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email text NOT NULL CHECK (email = lower(btrim(email))),
    role text NOT NULL CHECK (role IN ('parent','student')),
    parent_id bigint REFERENCES parents(id),
    student_id bigint REFERENCES students(id),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash)=32),
    created_by bigint NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    used_by bigint REFERENCES users(id),
    revoked_at timestamptz,
    CHECK ((role='parent' AND parent_id IS NOT NULL AND student_id IS NULL)
        OR (role='student' AND student_id IS NOT NULL AND parent_id IS NULL)),
    CHECK (expires_at=created_at + interval '48 hours'),
    CHECK ((used_at IS NULL)=(used_by IS NULL)),
    CHECK (used_at IS NULL OR revoked_at IS NULL)
);
CREATE UNIQUE INDEX invitations_pending_email ON invitations(email,role) WHERE used_at IS NULL AND revoked_at IS NULL;
CREATE UNIQUE INDEX invitations_pending_parent ON invitations(parent_id) WHERE used_at IS NULL AND revoked_at IS NULL;
CREATE UNIQUE INDEX invitations_pending_student ON invitations(student_id) WHERE used_at IS NULL AND revoked_at IS NULL;

-- Each identity has one account. Family relationships remain in student_parents
-- and are never inferred from contact information or invitation acceptance.
CREATE TABLE parent_accounts (
    parent_id bigint PRIMARY KEY REFERENCES parents(id),
    user_id bigint NOT NULL UNIQUE REFERENCES users(id),
    invitation_id bigint NOT NULL UNIQUE REFERENCES invitations(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE student_accounts (
    student_id bigint PRIMARY KEY REFERENCES students(id),
    user_id bigint NOT NULL UNIQUE REFERENCES users(id),
    invitation_id bigint NOT NULL UNIQUE REFERENCES invitations(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
