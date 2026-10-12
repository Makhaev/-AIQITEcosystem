CREATE TABLE programs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
    name_search text NOT NULL,
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 5000),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX programs_order ON programs(name_search,id);
CREATE TABLE program_modules (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    program_id bigint NOT NULL REFERENCES programs(id),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 5000),
    position integer NOT NULL CHECK (position > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT program_modules_position_unique UNIQUE(program_id,position),
    CONSTRAINT program_modules_identity UNIQUE(id,program_id)
);

-- Relationship foundation for subsequent group/schedule APIs. No group or lesson
-- mutation endpoint is exposed until its complete business rules are implemented.
CREATE TABLE groups (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
    program_id bigint NOT NULL REFERENCES programs(id),
    teacher_id bigint REFERENCES teachers(id),
    capacity integer NOT NULL CHECK (capacity > 0),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT groups_program_identity UNIQUE(id,program_id)
);
CREATE INDEX groups_program ON groups(program_id);
CREATE INDEX groups_teacher ON groups(teacher_id);
CREATE TABLE group_enrollments (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_id bigint NOT NULL REFERENCES groups(id),
    student_id bigint NOT NULL REFERENCES students(id),
    starts_on date NOT NULL,
    ends_on date CHECK (ends_on IS NULL OR ends_on >= starts_on),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX group_enrollments_student ON group_enrollments(student_id,group_id);
CREATE UNIQUE INDEX group_enrollments_current_unique ON group_enrollments(group_id,student_id) WHERE ends_on IS NULL;
CREATE TABLE lessons (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_id bigint NOT NULL,
    program_id bigint NOT NULL,
    module_id bigint,
    topic text NOT NULL CHECK (char_length(btrim(topic)) BETWEEN 1 AND 200),
    teacher_id bigint NOT NULL REFERENCES teachers(id),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL CHECK (ends_at > starts_at),
    location text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled','completed','cancelled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT lessons_group_program FOREIGN KEY(group_id,program_id) REFERENCES groups(id,program_id),
    CONSTRAINT lessons_module_program FOREIGN KEY(module_id,program_id) REFERENCES program_modules(id,program_id)
);
CREATE INDEX lessons_group ON lessons(group_id);
CREATE INDEX lessons_teacher ON lessons(teacher_id);
CREATE INDEX lessons_module ON lessons(module_id,program_id);

CREATE FUNCTION require_active_group_program() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE program_status text;
BEGIN
    IF TG_OP='UPDATE' THEN
        IF NEW.program_id IS NOT DISTINCT FROM OLD.program_id THEN RETURN NEW; END IF;
    END IF;
    -- Shares the program row lock with the archive/update transaction. New
    -- assignments cannot pass on an obsolete active status while archival runs.
    SELECT status INTO program_status FROM programs WHERE id=NEW.program_id FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Program does not exist' USING ERRCODE='23503', CONSTRAINT='groups_program_id_fkey';
    END IF;
    IF program_status <> 'active' THEN
        RAISE EXCEPTION 'New group assignment requires an active program' USING ERRCODE='23514', CONSTRAINT='groups_active_program';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER groups_active_program BEFORE INSERT OR UPDATE OF program_id ON groups
    FOR EACH ROW EXECUTE FUNCTION require_active_group_program();

CREATE FUNCTION derive_lesson_program() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE group_program bigint;
BEGIN
    SELECT program_id INTO group_program FROM groups WHERE id=NEW.group_id FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Group does not exist' USING ERRCODE='23503', CONSTRAINT='lessons_group_program';
    END IF;
    IF NEW.program_id IS NOT NULL AND NEW.program_id <> group_program THEN
        RAISE EXCEPTION 'Lesson program must match its group' USING ERRCODE='23514', CONSTRAINT='lessons_program_matches_group';
    END IF;
    NEW.program_id := group_program;
    RETURN NEW;
END $$;
CREATE TRIGGER lessons_derive_program BEFORE INSERT OR UPDATE OF group_id,program_id,module_id ON lessons
    FOR EACH ROW EXECUTE FUNCTION derive_lesson_program();
