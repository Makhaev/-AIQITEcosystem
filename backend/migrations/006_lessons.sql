CREATE EXTENSION IF NOT EXISTS btree_gist WITH SCHEMA public;

-- Native exclusion constraints check competing uncommitted tuples as well as
-- committed lessons. [) permits a lesson starting exactly when another ends.
ALTER TABLE lessons ADD CONSTRAINT lessons_teacher_overlap
    EXCLUDE USING gist (teacher_id public.gist_int8_ops WITH =, tstzrange(starts_at,ends_at,'[)') WITH &&)
    WHERE (status <> 'cancelled');
ALTER TABLE lessons ADD CONSTRAINT lessons_group_overlap
    EXCLUDE USING gist (group_id public.gist_int8_ops WITH =, tstzrange(starts_at,ends_at,'[)') WITH &&)
    WHERE (status <> 'cancelled');
ALTER TABLE lessons ADD COLUMN topic_search text NOT NULL DEFAULT '';
UPDATE lessons SET topic_search=lower(topic);
ALTER TABLE lessons ADD COLUMN roster_initialized_at timestamptz;
CREATE INDEX lessons_start_order ON lessons(starts_at,id);

ALTER TABLE attendance ADD COLUMN student_name text NOT NULL DEFAULT '';
UPDATE attendance a SET student_name=s.name FROM students s WHERE s.id=a.student_id;
ALTER TABLE attendance ADD COLUMN included boolean NOT NULL DEFAULT true;
-- Preserve any journal recorded before this migration, including its marks.
UPDATE lessons l SET roster_initialized_at=(SELECT min(a.created_at) FROM attendance a WHERE a.lesson_id=l.id)
    WHERE EXISTS(SELECT 1 FROM attendance a WHERE a.lesson_id=l.id);
ALTER TABLE audit_logs ADD COLUMN details jsonb NOT NULL DEFAULT '{}';

CREATE FUNCTION guard_lesson_roster() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.roster_initialized_at IS NOT NULL OR EXISTS(SELECT 1 FROM attendance WHERE lesson_id=OLD.id) THEN
        IF NEW.group_id <> OLD.group_id OR
           (NEW.starts_at AT TIME ZONE 'Europe/Moscow')::date <> (OLD.starts_at AT TIME ZONE 'Europe/Moscow')::date OR
           (OLD.roster_initialized_at IS NOT NULL AND NEW.roster_initialized_at IS DISTINCT FROM OLD.roster_initialized_at) THEN
            RAISE EXCEPTION 'The original journal group and date must be preserved'
                USING ERRCODE='23514', CONSTRAINT='lessons_roster_frozen';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER lessons_guard_roster BEFORE UPDATE ON lessons
    FOR EACH ROW EXECUTE FUNCTION guard_lesson_roster();

CREATE FUNCTION fill_attendance_name() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.student_name='' THEN
        SELECT name INTO NEW.student_name FROM students WHERE id=NEW.student_id;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER attendance_snapshot_name BEFORE INSERT ON attendance
    FOR EACH ROW EXECUTE FUNCTION fill_attendance_name();
