-- Source records, immutable content revisions, exact evidence revisions and decisions.
-- The Student 360 projection reads these sources; it does not maintain copies.
CREATE TABLE learning_records (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 student_id bigint NOT NULL REFERENCES students(id),
 kind text NOT NULL CHECK(kind IN ('goals','observations','assignments','attempts','projects','achievements')),
 lesson_id bigint REFERENCES lessons(id),
 scope text NOT NULL CHECK(scope IN ('lesson','curator','explicit')),
 parent_id bigint REFERENCES learning_records(id),
 parent_revision integer,
 reviewer_id bigint REFERENCES users(id),
 created_by bigint NOT NULL REFERENCES users(id),
 current_revision integer NOT NULL DEFAULT 1 CHECK(current_revision>0),
 state text NOT NULL,
 published boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 closed_at timestamptz,
 UNIQUE(id,student_id)
);
CREATE TABLE learning_revisions (
 record_id bigint NOT NULL REFERENCES learning_records(id),
 revision integer NOT NULL,
 content jsonb NOT NULL CHECK(jsonb_typeof(content)='object'),
 author_id bigint NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(record_id,revision)
);
CREATE TABLE learning_sources (
 record_id bigint NOT NULL,
 revision integer NOT NULL,
 source_id bigint NOT NULL,
 source_revision integer NOT NULL,
 PRIMARY KEY(record_id,revision,source_id,source_revision),
 FOREIGN KEY(record_id,revision) REFERENCES learning_revisions(record_id,revision),
 FOREIGN KEY(source_id,source_revision) REFERENCES learning_revisions(record_id,revision),
 CHECK(record_id<>source_id)
);
CREATE TABLE learning_files (
 record_id bigint NOT NULL,
 revision integer NOT NULL,
 file_id bigint NOT NULL REFERENCES private_files(id),
 PRIMARY KEY(record_id,revision,file_id),
 FOREIGN KEY(record_id,revision) REFERENCES learning_revisions(record_id,revision)
);
CREATE TABLE learning_decisions (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 record_id bigint NOT NULL REFERENCES learning_records(id),
 revision integer NOT NULL,
 action text NOT NULL,
 reason text NOT NULL,
 actor_id bigint NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE learning_grants (
 record_id bigint NOT NULL REFERENCES learning_records(id),
 user_id bigint NOT NULL REFERENCES users(id),
 PRIMARY KEY(record_id,user_id)
);
CREATE INDEX learning_student_kind ON learning_records(student_id,kind,created_at DESC,id DESC);
CREATE TRIGGER learning_revisions_immutable BEFORE UPDATE OR DELETE ON learning_revisions FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
CREATE TRIGGER learning_sources_immutable BEFORE UPDATE OR DELETE ON learning_sources FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
CREATE TRIGGER learning_files_immutable BEFORE UPDATE OR DELETE ON learning_files FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
CREATE TRIGGER learning_decisions_immutable BEFORE UPDATE OR DELETE ON learning_decisions FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
