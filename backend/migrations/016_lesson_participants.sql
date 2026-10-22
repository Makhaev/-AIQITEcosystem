-- Separate the immutable journal identity/saved composition from its mark.
-- Renaming preserves participant IDs, identity state, enrollment FKs and names.
ALTER TABLE attendance RENAME TO lesson_participants;
DROP TRIGGER attendance_guard_write ON lesson_participants;
ALTER TABLE lesson_participants RENAME CONSTRAINT attendance_pkey TO lesson_participants_pkey;
ALTER TABLE lesson_participants RENAME CONSTRAINT attendance_lesson_student_unique TO lesson_participants_lesson_student_unique;
ALTER TABLE lesson_participants RENAME CONSTRAINT attendance_lesson_group TO lesson_participants_lesson_group;
ALTER TABLE lesson_participants RENAME CONSTRAINT attendance_enrollment TO lesson_participants_enrollment;
ALTER INDEX attendance_enrollment_history RENAME TO lesson_participants_enrollment_history;
ALTER INDEX attendance_student_history RENAME TO lesson_participants_student_history;
ALTER SEQUENCE attendance_id_seq RENAME TO lesson_participants_id_seq;
ALTER FUNCTION fill_attendance_name() RENAME TO fill_lesson_participant_name;
ALTER TRIGGER attendance_snapshot_name ON lesson_participants RENAME TO lesson_participants_snapshot_name;

-- Shared primary key: a mark's ID remains the existing public participant ID.
-- No cascading deletes: history cannot disappear with a referenced identity.
CREATE TABLE attendance (
    id bigint PRIMARY KEY,
    status text NOT NULL DEFAULT 'unmarked'
        CHECK (status IN ('unmarked','present','absent','excused','late')),
    comment text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 500),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT attendance_participant_fkey FOREIGN KEY(id) REFERENCES lesson_participants(id)
);
INSERT INTO attendance(id,status,comment,created_at,updated_at)
SELECT id,status,comment,created_at,updated_at FROM lesson_participants;
ALTER TABLE lesson_participants DROP COLUMN status, DROP COLUMN comment;

-- A read model shared by reports and journals. It is not writable and does not
-- own the roster. A missing mark is represented as unmarked, never as absence.
CREATE VIEW attendance_records AS
SELECT p.id,p.lesson_id,p.group_id,p.enrollment_id,p.student_id,
       COALESCE(a.status,'unmarked') AS status,COALESCE(a.comment,'') AS comment,
       p.created_at,GREATEST(p.updated_at,a.updated_at) AS updated_at,
       p.student_name,p.included
FROM lesson_participants p LEFT JOIN attendance a ON a.id=p.id;

CREATE FUNCTION initialize_participant_mark() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO attendance(id,created_at,updated_at) VALUES(NEW.id,NEW.created_at,NEW.updated_at);
    RETURN NEW;
END $$;
CREATE TRIGGER lesson_participants_initialize_mark AFTER INSERT ON lesson_participants
    FOR EACH ROW EXECUTE FUNCTION initialize_participant_mark();

CREATE OR REPLACE FUNCTION guard_lesson_roster() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.roster_initialized_at IS NOT NULL OR EXISTS(SELECT 1 FROM lesson_participants WHERE lesson_id=OLD.id) THEN
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

CREATE OR REPLACE FUNCTION guard_lesson_status() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE completing boolean;
BEGIN
    completing := NEW.status='completed';
    IF TG_OP='UPDATE' THEN completing := completing AND OLD.status<>'completed'; END IF;
    IF NEW.status='completed' AND clock_timestamp()<NEW.ends_at THEN
        RAISE EXCEPTION 'Lesson has not ended'
            USING ERRCODE='23514', CONSTRAINT='lessons_completion_time';
    END IF;
    IF completing THEN
        IF EXISTS(SELECT 1 FROM attendance_records WHERE lesson_id=NEW.id AND included AND status='unmarked') OR
           (NEW.roster_initialized_at IS NULL AND EXISTS(
                SELECT 1 FROM group_enrollments WHERE group_id=NEW.group_id
                AND starts_on<=(NEW.starts_at AT TIME ZONE 'Europe/Moscow')::date
                AND (ends_on IS NULL OR ends_on>=(NEW.starts_at AT TIME ZONE 'Europe/Moscow')::date))) THEN
            RAISE EXCEPTION 'All included pupils must be marked before completion'
                USING ERRCODE='23514', CONSTRAINT='lessons_completion_unmarked';
        END IF;
    END IF;
    IF NEW.status='cancelled' AND EXISTS(SELECT 1 FROM attendance_records WHERE lesson_id=NEW.id AND status<>'unmarked') THEN
        RAISE EXCEPTION 'A lesson with attendance marks cannot be cancelled'
            USING ERRCODE='23514', CONSTRAINT='lessons_cancellation_marks';
    END IF;
    RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION guard_attendance_write() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE saved_lesson_id bigint; lesson_status text; lesson_start timestamptz;
BEGIN
    IF TG_OP='UPDATE' AND NEW.id<>OLD.id THEN
        RAISE EXCEPTION 'A mark belongs to its original participant'
            USING ERRCODE='23514', CONSTRAINT='attendance_participant_identity';
    END IF;
    SELECT lesson_id INTO saved_lesson_id FROM lesson_participants WHERE id=NEW.id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Participant does not exist'
            USING ERRCODE='23503', CONSTRAINT='attendance_participant_fkey';
    END IF;
    IF TG_OP='INSERT' AND NEW.status='unmarked' AND NEW.comment='' THEN RETURN NEW; END IF;
    -- Same lesson row/version as completion, cancel and reschedule writers.
    UPDATE lessons SET updated_at=clock_timestamp() WHERE id=saved_lesson_id
        RETURNING status,starts_at INTO lesson_status,lesson_start;
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
CREATE TRIGGER attendance_guard_write BEFORE INSERT OR UPDATE ON attendance
    FOR EACH ROW EXECUTE FUNCTION guard_attendance_write();
