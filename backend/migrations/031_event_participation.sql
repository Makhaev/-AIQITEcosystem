ALTER TABLE free_events DROP CONSTRAINT free_events_status_check;
ALTER TABLE free_events ADD CONSTRAINT free_events_status_check CHECK(status IN ('draft','published','registration_closed','completed','cancelled'));
ALTER TABLE free_events ADD COLUMN cancelled_at timestamptz;
CREATE OR REPLACE FUNCTION guard_free_event_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.status IN ('completed','cancelled') THEN RAISE EXCEPTION 'final event is immutable'; END IF;
 IF NEW.status<>OLD.status AND NOT (
  (OLD.status='draft' AND NEW.status='published') OR
  (OLD.status='published' AND NEW.status='registration_closed') OR
  (OLD.status='registration_closed' AND NEW.status='completed') OR
  (OLD.status IN ('published','registration_closed') AND NEW.status='cancelled')
 ) THEN RAISE EXCEPTION 'invalid event transition'; END IF;
 RETURN NEW;
END $$;

CREATE TABLE event_registrations (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 event_id bigint NOT NULL REFERENCES free_events(id),
 student_id bigint NOT NULL REFERENCES students(id),
 status text NOT NULL CHECK(status IN ('requested','confirmed','waitlisted','cancelled','attended','no_show')),
 revision bigint NOT NULL DEFAULT 1,
 created_by bigint NOT NULL REFERENCES users(id),
 updated_by bigint NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE UNIQUE INDEX event_registration_active ON event_registrations(event_id,student_id) WHERE status<>'cancelled';
CREATE INDEX event_registration_listing ON event_registrations(event_id,id);
CREATE TABLE event_registration_history (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 registration_id bigint NOT NULL REFERENCES event_registrations(id),
 status text NOT NULL, revision bigint NOT NULL,
 actor_id bigint NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(registration_id,revision)
);
CREATE TRIGGER event_history_immutable BEFORE UPDATE OR DELETE ON event_registration_history FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
CREATE TRIGGER event_registration_no_delete BEFORE DELETE ON event_registrations FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
CREATE FUNCTION record_event_registration() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO event_registration_history(registration_id,status,revision,actor_id) VALUES(NEW.id,NEW.status,NEW.revision,NEW.updated_by);
 RETURN NEW;
END $$;
CREATE TRIGGER event_registration_history AFTER INSERT OR UPDATE ON event_registrations FOR EACH ROW EXECUTE FUNCTION record_event_registration();

-- Participation documents are private to this registration and its current audience.
CREATE TABLE event_documents (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 registration_id bigint NOT NULL REFERENCES event_registrations(id),
 name text NOT NULL, media_type text NOT NULL, data bytea NOT NULL CHECK(octet_length(data) BETWEEN 1 AND 10485760),
 created_by bigint NOT NULL REFERENCES users(id), created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
ALTER TABLE notifications DROP CONSTRAINT notifications_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check CHECK(kind IN ('invitation','password_reset','lesson_rescheduled','lesson_cancelled','assignment_assigned','attempt_reviewed','report_published','request_answered','participation_confirmed','participation_cancelled','event_rescheduled','event_cancelled'));
