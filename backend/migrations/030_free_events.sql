CREATE TABLE free_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 title text NOT NULL CHECK(length(btrim(title)) BETWEEN 1 AND 200),
 title_search text NOT NULL,
 type text NOT NULL DEFAULT '' CHECK(length(type)<=100),
 description text NOT NULL DEFAULT '' CHECK(length(description)<=5000),
 organizer text NOT NULL DEFAULT '' CHECK(length(organizer)<=200),
 starts_at timestamptz, ends_at timestamptz,
 time_zone text NOT NULL DEFAULT 'Europe/Moscow' CHECK(length(time_zone) BETWEEN 1 AND 100),
 location text NOT NULL DEFAULT '' CHECK(length(location)<=1000),
 capacity integer NOT NULL DEFAULT 0 CHECK(capacity BETWEEN 0 AND 1000000),
 registration_starts_at timestamptz, registration_ends_at timestamptz,
 conditions text NOT NULL DEFAULT '' CHECK(length(conditions)<=5000),
 required_documents text NOT NULL DEFAULT '' CHECK(length(required_documents)<=5000),
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','published','registration_closed','completed')),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 created_by bigint NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 published_at timestamptz, registration_closed_at timestamptz, completed_at timestamptz,
 CHECK(starts_at IS NULL OR ends_at IS NULL OR starts_at<ends_at),
 CHECK(registration_starts_at IS NULL OR registration_ends_at IS NULL OR registration_starts_at<registration_ends_at),
 CHECK(registration_ends_at IS NULL OR starts_at IS NULL OR registration_ends_at<=starts_at),
 CHECK(status='draft' OR (length(btrim(type))>0 AND length(btrim(description))>0 AND length(btrim(organizer))>0 AND length(btrim(location))>0 AND length(btrim(conditions))>0 AND capacity>0 AND starts_at IS NOT NULL AND ends_at IS NOT NULL AND registration_starts_at IS NOT NULL AND registration_ends_at IS NOT NULL AND published_at IS NOT NULL))
);
CREATE TABLE free_event_responsibles (
 event_id bigint NOT NULL REFERENCES free_events(id),
 user_id bigint NOT NULL REFERENCES users(id),
 PRIMARY KEY(event_id,user_id)
);
CREATE INDEX free_events_listing ON free_events(status,starts_at,id);
CREATE FUNCTION guard_free_event_transition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.status='completed' THEN RAISE EXCEPTION 'completed event is immutable'; END IF;
 IF NEW.status<>OLD.status AND NOT (
  (OLD.status='draft' AND NEW.status='published') OR
  (OLD.status='published' AND NEW.status='registration_closed') OR
  (OLD.status='registration_closed' AND NEW.status='completed')
 ) THEN RAISE EXCEPTION 'invalid event transition'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER free_events_transitions BEFORE UPDATE ON free_events FOR EACH ROW EXECUTE FUNCTION guard_free_event_transition();
