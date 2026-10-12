ALTER TABLE groups ADD COLUMN name_search text NOT NULL DEFAULT '';
UPDATE groups SET name_search=lower(name);
CREATE INDEX groups_order ON groups(name_search,id);
CREATE INDEX group_enrollments_group_history ON group_enrollments(group_id,starts_on,id);

-- Capacity and enrollment writers share the group row lock. Updating its version
-- also makes stale REPEATABLE READ writers fail instead of admitting too many pupils.
CREATE FUNCTION guard_group_changes() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.capacity < OLD.capacity AND NEW.capacity <
       (SELECT count(*) FROM group_enrollments WHERE group_id=OLD.id AND ends_on IS NULL) THEN
        RAISE EXCEPTION 'Capacity is below current enrollment count'
            USING ERRCODE='23514', CONSTRAINT='groups_capacity_exceeded';
    END IF;
    IF NEW.program_id <> OLD.program_id AND
       (EXISTS(SELECT 1 FROM group_enrollments WHERE group_id=OLD.id) OR
        EXISTS(SELECT 1 FROM lessons WHERE group_id=OLD.id)) THEN
        RAISE EXCEPTION 'A group with history cannot change program'
            USING ERRCODE='23514', CONSTRAINT='groups_program_has_history';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER groups_guard_changes BEFORE UPDATE ON groups
    FOR EACH ROW EXECUTE FUNCTION guard_group_changes();

CREATE FUNCTION guard_group_enrollment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE group_status text; group_capacity integer; student_status text;
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'Enrollment history cannot be deleted'
            USING ERRCODE='23514', CONSTRAINT='enrollments_history_immutable';
    END IF;
    IF TG_OP='UPDATE' THEN
        IF NEW.id <> OLD.id OR NEW.group_id <> OLD.group_id OR NEW.student_id <> OLD.student_id
           OR NEW.starts_on <> OLD.starts_on OR NEW.created_at <> OLD.created_at
           OR (OLD.ends_on IS NOT NULL AND NEW.ends_on IS DISTINCT FROM OLD.ends_on) THEN
            RAISE EXCEPTION 'Close an enrollment and create a new one to transfer'
                USING ERRCODE='23514', CONSTRAINT='enrollments_history_immutable';
        END IF;
    END IF;
    UPDATE groups SET updated_at=clock_timestamp() WHERE id=NEW.group_id
        RETURNING status,capacity INTO group_status,group_capacity;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Group does not exist' USING ERRCODE='23503', CONSTRAINT='group_enrollments_group_id_fkey';
    END IF;
    IF TG_OP='INSERT' AND NEW.ends_on IS NULL THEN
        IF group_status <> 'active' THEN
            RAISE EXCEPTION 'Enrollment requires an active group'
                USING ERRCODE='23514', CONSTRAINT='enrollments_active_group';
        END IF;
        SELECT status INTO student_status FROM students WHERE id=NEW.student_id FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'Student does not exist' USING ERRCODE='23503', CONSTRAINT='group_enrollments_student_id_fkey';
        END IF;
        IF student_status <> 'active' THEN
            RAISE EXCEPTION 'Enrollment requires an active student'
                USING ERRCODE='23514', CONSTRAINT='enrollments_active_student';
        END IF;
        IF EXISTS(SELECT 1 FROM group_enrollments WHERE group_id=NEW.group_id AND student_id=NEW.student_id AND ends_on IS NULL) THEN
            RAISE EXCEPTION 'Already enrolled' USING ERRCODE='23505', CONSTRAINT='group_enrollments_current_unique';
        END IF;
        IF (SELECT count(*) FROM group_enrollments WHERE group_id=NEW.group_id AND ends_on IS NULL) >= group_capacity THEN
            RAISE EXCEPTION 'Group is full' USING ERRCODE='23514', CONSTRAINT='groups_capacity_exceeded';
        END IF;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER group_enrollments_guard BEFORE INSERT OR UPDATE OR DELETE ON group_enrollments
    FOR EACH ROW EXECUTE FUNCTION guard_group_enrollment();

-- Attendance belongs to the original lesson and enrollment, never to a pupil's
-- current group. Composite FKs prevent pairing records from different groups.
ALTER TABLE group_enrollments ADD CONSTRAINT enrollment_identity UNIQUE(id,group_id,student_id);
ALTER TABLE lessons ADD CONSTRAINT lesson_group_identity UNIQUE(id,group_id);
CREATE TABLE attendance (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    lesson_id bigint NOT NULL,
    group_id bigint NOT NULL,
    enrollment_id bigint NOT NULL,
    student_id bigint NOT NULL,
    status text NOT NULL DEFAULT 'unmarked' CHECK (status IN ('unmarked','present','absent','excused','late')),
    comment text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 500),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT attendance_lesson_student_unique UNIQUE(lesson_id,student_id),
    CONSTRAINT attendance_lesson_group FOREIGN KEY(lesson_id,group_id) REFERENCES lessons(id,group_id),
    CONSTRAINT attendance_enrollment FOREIGN KEY(enrollment_id,group_id,student_id) REFERENCES group_enrollments(id,group_id,student_id)
);
CREATE INDEX attendance_enrollment_history ON attendance(enrollment_id);
