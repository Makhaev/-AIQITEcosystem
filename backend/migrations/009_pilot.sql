-- Additive pilot schema: existing accounts, families and learning history are retained.
ALTER TABLE user_roles DROP CONSTRAINT user_roles_role_check;
ALTER TABLE user_roles ADD CONSTRAINT user_roles_role_check CHECK (role IN ('admin','leader','teacher','curator','student','parent'));
ALTER TABLE students ADD COLUMN user_id bigint UNIQUE REFERENCES users(id);
ALTER TABLE parents ADD COLUMN user_id bigint UNIQUE REFERENCES users(id);

CREATE TABLE access_links (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash)=32),
    user_id bigint NOT NULL REFERENCES users(id),
    purpose text NOT NULL CHECK (purpose IN ('invitation','recovery')),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX access_links_user ON access_links(user_id);
CREATE TABLE access_requests (
    user_id bigint PRIMARY KEY REFERENCES users(id),
    requested_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);

CREATE TABLE assignments (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    lesson_id bigint NOT NULL REFERENCES lessons(id),
    student_id bigint NOT NULL REFERENCES students(id),
    title text NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 200),
    instructions text NOT NULL CHECK (char_length(btrim(instructions)) BETWEEN 1 AND 10000),
    due_at timestamptz,
    status text NOT NULL DEFAULT 'assigned' CHECK (status IN ('assigned','submitted','reviewed')),
    answer text NOT NULL DEFAULT '' CHECK (char_length(answer)<=10000),
    feedback text NOT NULL DEFAULT '' CHECK (char_length(feedback)<=5000),
    submitted_at timestamptz,
    reviewed_at timestamptz,
    created_by bigint NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (status='assigned' OR char_length(btrim(answer))>0),
    CHECK (status<>'reviewed' OR char_length(btrim(feedback))>0)
);
CREATE INDEX assignments_student ON assignments(student_id,lesson_id);
CREATE TABLE learning_reports (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    lesson_id bigint NOT NULL REFERENCES lessons(id),
    student_id bigint NOT NULL REFERENCES students(id),
    summary text NOT NULL CHECK (char_length(btrim(summary)) BETWEEN 1 AND 10000),
    published_at timestamptz,
    created_by bigint NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(lesson_id,student_id)
);
