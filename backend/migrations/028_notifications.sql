CREATE TABLE notifications (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id),
 student_id bigint REFERENCES students(id),
 kind text NOT NULL CHECK (kind IN ('invitation','password_reset','lesson_rescheduled','lesson_cancelled','assignment_assigned','attempt_reviewed','report_published','request_answered','participation_confirmed','participation_cancelled')),
 object_id bigint NOT NULL CHECK (object_id>0),
 event_version text NOT NULL,
 audience text NOT NULL CHECK (audience IN ('account','family','teacher')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 read_at timestamptz,
 email_state text NOT NULL DEFAULT 'pending' CHECK (email_state IN ('pending','sent','suppressed','external')),
 attempts integer NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 sent_at timestamptz
);
CREATE UNIQUE INDEX notifications_event_recipient ON notifications(kind,object_id,event_version,user_id,COALESCE(student_id,0),audience);
CREATE INDEX notifications_inbox ON notifications(user_id,id DESC);
CREATE INDEX notifications_delivery ON notifications(next_attempt_at,id) WHERE email_state='pending';

-- Shared by recipient selection, inbox access and delivery-time authorization.
CREATE FUNCTION notification_family_access(uid bigint, sid bigint) RETURNS boolean
LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM students s JOIN users u ON u.id=uid AND NOT u.blocked
 WHERE s.id=sid AND s.status='active' AND (
 EXISTS(SELECT 1 FROM student_accounts sa JOIN user_roles ur ON ur.user_id=sa.user_id AND ur.role='student' WHERE sa.user_id=uid AND sa.student_id=sid)
 OR EXISTS(SELECT 1 FROM parent_accounts pa JOIN parents p ON p.id=pa.parent_id AND p.status='active'
 JOIN student_parents sp ON sp.parent_id=p.id AND sp.student_id=sid
 JOIN family_access fa ON fa.student_id=sp.student_id AND fa.parent_id=sp.parent_id AND fa.confirmed
 JOIN user_roles ur ON ur.user_id=pa.user_id AND ur.role='parent' WHERE pa.user_id=uid)))
$$;
