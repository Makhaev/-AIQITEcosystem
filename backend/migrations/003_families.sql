CREATE TABLE students (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
    name_search text NOT NULL,
    birth_date date,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    curator_id bigint REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX students_curator ON students(curator_id);
CREATE INDEX students_order ON students(name_search,id);
CREATE TABLE parents (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
    name_search text NOT NULL,
    phone text NOT NULL DEFAULT '',
    email text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT parents_contact_required CHECK (btrim(phone) <> '' OR btrim(email) <> '')
);
CREATE INDEX parents_order ON parents(name_search,id);
CREATE TABLE student_parents (
    student_id bigint NOT NULL REFERENCES students(id),
    parent_id bigint NOT NULL REFERENCES parents(id),
    relationship text NOT NULL CHECK (char_length(btrim(relationship)) BETWEEN 1 AND 100),
    is_primary boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT student_parents_unique PRIMARY KEY(student_id,parent_id)
);
CREATE UNIQUE INDEX student_parents_one_primary ON student_parents(student_id) WHERE is_primary;
CREATE INDEX student_parents_parent ON student_parents(parent_id,student_id);
