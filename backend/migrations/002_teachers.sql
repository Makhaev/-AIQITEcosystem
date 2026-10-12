CREATE TABLE teachers (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    name_search text NOT NULL CHECK (name_search <> ''),
    phone text NOT NULL DEFAULT '' CHECK (phone = '' OR phone ~ '^\+[1-9][0-9]{6,14}$'),
    email text NOT NULL DEFAULT '' CHECK (length(email) <= 254),
    specialization text NOT NULL DEFAULT '' CHECK (length(specialization) <= 500),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    user_id bigint REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX teachers_user_unique ON teachers(user_id) WHERE user_id IS NOT NULL;
CREATE INDEX teachers_name_order ON teachers(name_search, id);
CREATE INDEX teachers_status ON teachers(status);

-- Audit objects are now polymorphic; the actor still references users.
-- Existing rows are user/auth events and retain their original object IDs.
ALTER TABLE audit_logs ADD COLUMN object_type text NOT NULL DEFAULT 'user';
ALTER TABLE audit_logs DROP CONSTRAINT audit_logs_object_id_fkey;
CREATE INDEX audit_logs_object ON audit_logs(object_type, object_id, occurred_at);
