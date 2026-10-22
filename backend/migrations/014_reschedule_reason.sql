-- Old reschedules retain their original dates and rosters; the reason is unknown.
ALTER TABLE lesson_reschedules ADD COLUMN reason text
    CHECK (reason IS NULL OR char_length(btrim(reason)) BETWEEN 1 AND 1000);

CREATE FUNCTION guard_lesson_reschedules() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Lesson reschedule history is append-only'
      USING ERRCODE='23514', CONSTRAINT='lesson_reschedules_append_only';
END $$;
CREATE TRIGGER lesson_reschedules_append_only BEFORE UPDATE OR DELETE ON lesson_reschedules
    FOR EACH ROW EXECUTE FUNCTION guard_lesson_reschedules();
