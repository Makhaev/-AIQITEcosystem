CREATE TABLE branches (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 name text NOT NULL CHECK(char_length(btrim(name)) BETWEEN 1 AND 200),
 status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','archived')),
 created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE groups ADD COLUMN branch_id bigint REFERENCES branches(id);
ALTER TABLE groups ADD COLUMN curator_id bigint REFERENCES users(id);
CREATE INDEX groups_branch ON groups(branch_id);
CREATE INDEX groups_curator ON groups(curator_id);
ALTER TABLE students ADD COLUMN phone text NOT NULL DEFAULT '';
ALTER TABLE students ADD COLUMN email text NOT NULL DEFAULT '';

-- Current group curators take precedence; individual assignment is a fallback.
-- Multiple current groups may have different curators, each with pupil access.
CREATE FUNCTION effective_curators(pupil bigint) RETURNS bigint[] LANGUAGE sql STABLE AS $$
 SELECT CASE WHEN cardinality(current_ids)>0 THEN current_ids
 ELSE ARRAY(SELECT curator_id FROM students WHERE id=pupil AND curator_id IS NOT NULL) END
 FROM (SELECT ARRAY(SELECT DISTINCT g.curator_id FROM group_enrollments e JOIN groups g ON g.id=e.group_id
 WHERE e.student_id=pupil AND g.status='active' AND g.curator_id IS NOT NULL
 AND e.starts_on<=(now() AT TIME ZONE 'Europe/Moscow')::date
 AND (e.ends_on IS NULL OR e.ends_on>=(now() AT TIME ZONE 'Europe/Moscow')::date)) current_ids) v
$$;
CREATE FUNCTION curator_has_student(actor bigint,pupil bigint) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT actor=ANY(effective_curators(pupil))
$$;

CREATE TABLE curator_notes (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 student_id bigint NOT NULL REFERENCES students(id),
 author_id bigint NOT NULL REFERENCES users(id),
 body text NOT NULL CHECK(char_length(btrim(body)) BETWEEN 1 AND 2000),
 version integer NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX curator_notes_student ON curator_notes(student_id,id DESC);
CREATE TABLE curator_note_revisions (
 note_id bigint NOT NULL REFERENCES curator_notes(id),
 version integer NOT NULL,
 editor_id bigint NOT NULL REFERENCES users(id),
 body text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(note_id,version)
);
