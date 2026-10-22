-- Previously recorded versions remain intact; their correction reason is unknown.
ALTER TABLE lesson_snapshot_versions ADD COLUMN reason text
    CHECK (reason IS NULL OR char_length(btrim(reason)) BETWEEN 1 AND 1000);

CREATE OR REPLACE FUNCTION preserve_lesson_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.snapshot_state IN ('captured','legacy_unknown') AND
       ROW(NEW.snapshot_program_name,NEW.snapshot_module_name,NEW.snapshot_topic,NEW.snapshot_state,NEW.snapshot_captured_at)
       IS DISTINCT FROM ROW(OLD.snapshot_program_name,OLD.snapshot_module_name,OLD.snapshot_topic,OLD.snapshot_state,OLD.snapshot_captured_at) THEN
        INSERT INTO lesson_snapshot_versions(lesson_id,actor_id,program_name,module_name,topic,captured_at,state,reason)
        VALUES(OLD.id,NULLIF(current_setting('aiq.snapshot_actor',true),'')::bigint,
               OLD.snapshot_program_name,OLD.snapshot_module_name,OLD.snapshot_topic,OLD.snapshot_captured_at,OLD.snapshot_state,
               NULLIF(current_setting('aiq.snapshot_reason',true),''));
    END IF;
    RETURN NEW;
END $$;

CREATE FUNCTION guard_lesson_snapshot_versions() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Lesson snapshot history is append-only'
      USING ERRCODE='23514', CONSTRAINT='lesson_snapshot_versions_append_only';
END $$;
CREATE TRIGGER lesson_snapshot_versions_append_only BEFORE UPDATE OR DELETE ON lesson_snapshot_versions
    FOR EACH ROW EXECUTE FUNCTION guard_lesson_snapshot_versions();
