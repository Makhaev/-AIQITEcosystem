-- Existing contact links are deliberately not confirmed for portal access.
CREATE TABLE family_access (
 student_id bigint NOT NULL,
 parent_id bigint NOT NULL,
 confirmed boolean NOT NULL DEFAULT false,
 changed_by bigint NOT NULL REFERENCES users(id),
 changed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(student_id,parent_id),
 FOREIGN KEY(student_id,parent_id) REFERENCES student_parents(student_id,parent_id)
);
CREATE TABLE family_learning_publications (
 record_id bigint PRIMARY KEY REFERENCES learning_records(id),
 revision integer NOT NULL,
 students boolean NOT NULL,
 parents boolean NOT NULL,
 changed_by bigint NOT NULL REFERENCES users(id),
 FOREIGN KEY(record_id,revision) REFERENCES learning_revisions(record_id,revision)
);
CREATE TABLE family_file_access (
 file_id bigint PRIMARY KEY REFERENCES private_files(id),
 students boolean NOT NULL,
 parents boolean NOT NULL,
 changed_by bigint NOT NULL REFERENCES users(id)
);
CREATE TABLE family_reports (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 student_id bigint NOT NULL REFERENCES students(id),
 title text NOT NULL,
 body text NOT NULL,
 revision integer NOT NULL DEFAULT 1,
 published boolean NOT NULL DEFAULT false,
 created_by bigint NOT NULL REFERENCES users(id),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE family_report_files (
 report_id bigint NOT NULL REFERENCES family_reports(id),
 file_id bigint NOT NULL REFERENCES private_files(id),
 PRIMARY KEY(report_id,file_id)
);
CREATE TABLE family_report_history (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 report_id bigint NOT NULL REFERENCES family_reports(id),
 revision integer NOT NULL,
 content jsonb NOT NULL,
 actor_id bigint NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(report_id,revision)
);
CREATE TRIGGER family_report_history_immutable BEFORE UPDATE OR DELETE ON family_report_history FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
CREATE TABLE family_requests (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 student_id bigint NOT NULL REFERENCES students(id),
 created_by bigint NOT NULL REFERENCES users(id),
 subject text NOT NULL,
 body text NOT NULL,
 reply text NOT NULL DEFAULT '',
 revision integer NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX family_requests_student ON family_requests(student_id,id);
CREATE INDEX family_reports_student ON family_reports(student_id,id);
