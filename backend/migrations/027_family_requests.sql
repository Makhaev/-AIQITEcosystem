ALTER TABLE family_requests ALTER COLUMN student_id DROP NOT NULL;
ALTER TABLE family_requests
 ADD COLUMN assignee_id bigint REFERENCES users(id),
 ADD COLUMN status text NOT NULL DEFAULT 'new' CHECK(status IN ('new','assigned','in_progress','answered','closed')),
 ADD COLUMN answered_at timestamptz,
 ADD COLUMN closed_at timestamptz;
UPDATE family_requests SET status='answered' WHERE reply<>'';
CREATE TABLE family_request_messages (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 request_id bigint NOT NULL REFERENCES family_requests(id),
 author_id bigint REFERENCES users(id),
 body text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('message','answer')),
 created_at timestamptz
);
INSERT INTO family_request_messages(request_id,author_id,body,kind,created_at)
 SELECT id,created_by,body,'message',created_at FROM family_requests;
-- Old replies had no recorded author or timestamp. Preserve that uncertainty.
INSERT INTO family_request_messages(request_id,body,kind) SELECT id,reply,'answer' FROM family_requests WHERE reply<>'';
CREATE TABLE family_request_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 request_id bigint NOT NULL REFERENCES family_requests(id),
 action text NOT NULL, from_status text NOT NULL, to_status text NOT NULL,
 actor_id bigint REFERENCES users(id), assignee_id bigint REFERENCES users(id),
 reason text NOT NULL DEFAULT '', created_at timestamptz
);
INSERT INTO family_request_events(request_id,action,from_status,to_status,actor_id,created_at)
 SELECT id,'create','','new',created_by,created_at FROM family_requests;
INSERT INTO family_request_events(request_id,action,from_status,to_status)
 SELECT id,'legacy_answer','new','answered' FROM family_requests WHERE reply<>'';
CREATE TABLE family_request_files (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 message_id bigint NOT NULL REFERENCES family_request_messages(id),
 name text NOT NULL, media_type text NOT NULL, data bytea NOT NULL CHECK(octet_length(data) BETWEEN 1 AND 10485760)
);
CREATE INDEX family_requests_author ON family_requests(created_by,id);
CREATE INDEX family_requests_assignee ON family_requests(assignee_id,status,id);
CREATE INDEX family_request_messages_thread ON family_request_messages(request_id,id);
CREATE INDEX family_request_events_thread ON family_request_events(request_id,id);
CREATE INDEX family_request_files_message ON family_request_files(message_id,id);
CREATE TRIGGER family_request_messages_immutable BEFORE UPDATE OR DELETE ON family_request_messages FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
CREATE TRIGGER family_request_events_immutable BEFORE UPDATE OR DELETE ON family_request_events FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
CREATE TRIGGER family_request_files_immutable BEFORE UPDATE OR DELETE ON family_request_files FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
