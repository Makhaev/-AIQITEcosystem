ALTER TABLE lessons ADD COLUMN snapshot_program_name text;
ALTER TABLE lessons ADD COLUMN snapshot_module_name text;
ALTER TABLE lessons ADD COLUMN snapshot_topic text;
ALTER TABLE lessons ADD COLUMN snapshot_captured_at timestamptz;
ALTER TABLE lessons ADD COLUMN snapshot_state text NOT NULL DEFAULT 'not_captured'
    CHECK (snapshot_state IN ('not_captured','captured','legacy_unknown'));
UPDATE lessons SET snapshot_state='legacy_unknown' WHERE status='completed';
ALTER TABLE lessons ADD CONSTRAINT lessons_snapshot_complete CHECK
    (snapshot_state<>'captured' OR (snapshot_program_name IS NOT NULL AND snapshot_topic IS NOT NULL AND snapshot_captured_at IS NOT NULL));

CREATE TABLE lesson_snapshot_versions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    lesson_id bigint NOT NULL REFERENCES lessons(id),
    actor_id bigint REFERENCES users(id),
    program_name text,
    module_name text,
    topic text,
    captured_at timestamptz,
    state text NOT NULL,
    preserved_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX lesson_snapshot_versions_order ON lesson_snapshot_versions(lesson_id,id);

CREATE FUNCTION preserve_lesson_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.snapshot_state IN ('captured','legacy_unknown') AND
       ROW(NEW.snapshot_program_name,NEW.snapshot_module_name,NEW.snapshot_topic,NEW.snapshot_state)
       IS DISTINCT FROM ROW(OLD.snapshot_program_name,OLD.snapshot_module_name,OLD.snapshot_topic,OLD.snapshot_state) THEN
        INSERT INTO lesson_snapshot_versions(lesson_id,actor_id,program_name,module_name,topic,captured_at,state)
        VALUES(OLD.id,NULLIF(current_setting('aiq.snapshot_actor',true),'')::bigint,
               OLD.snapshot_program_name,OLD.snapshot_module_name,OLD.snapshot_topic,OLD.snapshot_captured_at,OLD.snapshot_state);
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER lessons_preserve_snapshot BEFORE UPDATE ON lessons
    FOR EACH ROW EXECUTE FUNCTION preserve_lesson_snapshot();

CREATE TABLE lesson_reschedules (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    lesson_id bigint NOT NULL REFERENCES lessons(id),
    actor_id bigint NOT NULL REFERENCES users(id),
    old_starts_at timestamptz NOT NULL,
    old_ends_at timestamptz NOT NULL,
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    previous_roster bigint[] NOT NULL,
    roster bigint[] NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE OR REPLACE FUNCTION guard_lesson_roster() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.roster_initialized_at IS NOT NULL OR EXISTS(SELECT 1 FROM attendance WHERE lesson_id=OLD.id) THEN
        IF NEW.group_id<>OLD.group_id OR
           (OLD.roster_initialized_at IS NOT NULL AND NEW.roster_initialized_at IS DISTINCT FROM OLD.roster_initialized_at) OR
           ((NEW.starts_at AT TIME ZONE 'Europe/Moscow')::date<>(OLD.starts_at AT TIME ZONE 'Europe/Moscow')::date AND
            (OLD.status<>'scheduled' OR NEW.status<>'scheduled' OR
             current_setting('aiq.roster_reschedule',true) IS DISTINCT FROM OLD.id::text)) THEN
            RAISE EXCEPTION 'Explicit roster reschedule required'
                USING ERRCODE='23514', CONSTRAINT='lessons_roster_frozen';
        END IF;
    END IF;
    RETURN NEW;
END $$;
