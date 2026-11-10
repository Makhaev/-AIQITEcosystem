CREATE TABLE student_status_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    student_id bigint NOT NULL REFERENCES students(id),
    status text NOT NULL CHECK(status IN ('active','archived')),
    changed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX student_status_events_period ON student_status_events(student_id,changed_at DESC,id DESC);

-- Recover the initial state and recorded status transitions of existing pupils.
INSERT INTO student_status_events(student_id,status,changed_at)
SELECT s.id,COALESCE((SELECT CASE a.action WHEN 'student.archived' THEN 'active' ELSE 'archived' END
 FROM audit_logs a WHERE a.object_type='student' AND a.object_id=s.id
 AND a.action IN ('student.archived','student.restored') ORDER BY a.occurred_at,a.id LIMIT 1),s.status),s.created_at
FROM students s;
INSERT INTO student_status_events(student_id,status,changed_at)
SELECT s.id,CASE a.action WHEN 'student.archived' THEN 'archived' ELSE 'active' END,a.occurred_at
FROM audit_logs a JOIN students s ON s.id=a.object_id
WHERE a.object_type='student' AND a.action IN ('student.archived','student.restored')
AND a.occurred_at>=s.created_at ORDER BY a.occurred_at,a.id;
INSERT INTO student_status_events(student_id,status,changed_at)
SELECT s.id,s.status,now() FROM students s WHERE s.status IS DISTINCT FROM
(SELECT e.status FROM student_status_events e WHERE e.student_id=s.id ORDER BY e.changed_at DESC,e.id DESC LIMIT 1);

CREATE FUNCTION record_student_status_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        INSERT INTO student_status_events(student_id,status,changed_at) VALUES(NEW.id,NEW.status,NEW.created_at);
    ELSIF NEW.status IS DISTINCT FROM OLD.status THEN
        INSERT INTO student_status_events(student_id,status) VALUES(NEW.id,NEW.status);
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER student_register_status AFTER INSERT OR UPDATE OF status ON students
FOR EACH ROW EXECUTE FUNCTION record_student_status_event();
