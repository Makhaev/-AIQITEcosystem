CREATE FUNCTION guard_lesson_status() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE completing boolean;
BEGIN
    completing := NEW.status='completed';
    IF TG_OP='UPDATE' THEN completing := completing AND OLD.status<>'completed'; END IF;
    IF NEW.status='completed' AND clock_timestamp()<NEW.ends_at THEN
        RAISE EXCEPTION 'Lesson has not ended'
            USING ERRCODE='23514', CONSTRAINT='lessons_completion_time';
    END IF;
    IF completing THEN
        IF EXISTS(SELECT 1 FROM attendance WHERE lesson_id=NEW.id AND included AND status='unmarked') OR
           (NEW.roster_initialized_at IS NULL AND EXISTS(
                SELECT 1 FROM group_enrollments WHERE group_id=NEW.group_id
                AND starts_on<=(NEW.starts_at AT TIME ZONE 'Europe/Moscow')::date
                AND (ends_on IS NULL OR ends_on>=(NEW.starts_at AT TIME ZONE 'Europe/Moscow')::date))) THEN
            RAISE EXCEPTION 'All included pupils must be marked before completion'
                USING ERRCODE='23514', CONSTRAINT='lessons_completion_unmarked';
        END IF;
    END IF;
    IF NEW.status='cancelled' AND EXISTS(SELECT 1 FROM attendance WHERE lesson_id=NEW.id AND status<>'unmarked') THEN
        RAISE EXCEPTION 'A lesson with attendance marks cannot be cancelled'
            USING ERRCODE='23514', CONSTRAINT='lessons_cancellation_marks';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER lessons_guard_status BEFORE INSERT OR UPDATE ON lessons
    FOR EACH ROW EXECUTE FUNCTION guard_lesson_status();

CREATE FUNCTION guard_attendance_write() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE lesson_status text; lesson_start timestamptz;
BEGIN
    -- Initial unmarked snapshot rows are allowed before the lesson starts.
    IF TG_OP='INSERT' AND NEW.status='unmarked' AND NEW.comment='' THEN RETURN NEW; END IF;
    -- Use the same row/version as status transitions. Competing marking/cancel
    -- transactions cannot both pass against obsolete state, even with RR.
    UPDATE lessons SET updated_at=clock_timestamp() WHERE id=NEW.lesson_id
        RETURNING status,starts_at INTO lesson_status,lesson_start;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Lesson does not exist' USING ERRCODE='23503', CONSTRAINT='attendance_lesson_group';
    END IF;
    IF lesson_status='cancelled' THEN
        RAISE EXCEPTION 'Cancelled lesson cannot accept attendance'
            USING ERRCODE='23514', CONSTRAINT='attendance_lesson_cancelled';
    END IF;
    IF clock_timestamp()<lesson_start THEN
        RAISE EXCEPTION 'Lesson has not started'
            USING ERRCODE='23514', CONSTRAINT='attendance_lesson_not_started';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER attendance_guard_write BEFORE INSERT OR UPDATE OF status,comment ON attendance
    FOR EACH ROW EXECUTE FUNCTION guard_attendance_write();
