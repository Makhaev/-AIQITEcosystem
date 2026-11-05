ALTER TABLE groups ADD COLUMN curator_id bigint REFERENCES users(id);
CREATE INDEX groups_curator ON groups(curator_id);

-- Only migrate unanimous, complete legacy assignments. A mixed group must be
-- assigned explicitly; choosing one of its old curators would widen access.
UPDATE groups g SET curator_id=c.curator_id
FROM (
    SELECT e.group_id,min(s.curator_id) AS curator_id
    FROM group_enrollments e JOIN students s ON s.id=e.student_id
    WHERE e.ends_on IS NULL AND e.starts_on<=(now() AT TIME ZONE 'Europe/Moscow')::date
    GROUP BY e.group_id
    HAVING count(DISTINCT s.curator_id)=1 AND count(s.curator_id)=count(*)
) c
WHERE g.id=c.group_id AND EXISTS(SELECT 1 FROM users u JOIN user_roles r ON r.user_id=u.id WHERE u.id=c.curator_id AND NOT u.blocked AND r.role='curator');

-- The old students.curator_id is retained as migration history only. It is no
-- longer an authorization source and cannot be assigned through the student API.
CREATE FUNCTION curator_has_student(actor bigint,pupil bigint) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS(SELECT 1 FROM group_enrollments e JOIN groups g ON g.id=e.group_id
        WHERE e.student_id=pupil AND g.curator_id=actor AND g.status='active'
        AND e.ends_on IS NULL AND e.starts_on<=(now() AT TIME ZONE 'Europe/Moscow')::date)
$$;
CREATE FUNCTION student_curators(pupil bigint) RETURNS jsonb
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(jsonb_agg(jsonb_build_object('id',c.id,'name',c.name) ORDER BY c.name,c.id),'[]'::jsonb)
    FROM (SELECT DISTINCT u.id,u.name FROM group_enrollments e JOIN groups g ON g.id=e.group_id
        JOIN users u ON u.id=g.curator_id WHERE e.student_id=pupil AND g.status='active'
        AND e.ends_on IS NULL AND e.starts_on<=(now() AT TIME ZONE 'Europe/Moscow')::date) c
$$;
