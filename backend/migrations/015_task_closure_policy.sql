-- Deterministic policy shared by all writes through the lifecycle trigger.
-- NULL previous_status denotes creation. Legacy closed dates remain unknown.
CREATE FUNCTION task_closure_time(previous_status text, next_status text,
    previous_closed_at timestamptz, changed_at timestamptz)
RETURNS timestamptz LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE
        WHEN previous_status IS NULL THEN NULL
        WHEN next_status = previous_status THEN previous_closed_at
        WHEN next_status = 'in_progress' THEN NULL
        WHEN previous_status IN ('todo','in_progress') THEN changed_at
        ELSE previous_closed_at
    END
$$;

CREATE OR REPLACE FUNCTION guard_task_lifecycle() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.status<>'todo' THEN
            RAISE EXCEPTION 'Tasks start in todo' USING ERRCODE='23514', CONSTRAINT='tasks_initial_status';
        END IF;
        NEW.closed_at=task_closure_time(NULL,NEW.status,NULL,clock_timestamp());
    ELSE
        IF NEW.status IS DISTINCT FROM OLD.status AND NOT (
            (OLD.status='todo' AND NEW.status IN ('in_progress','cancelled')) OR
            (OLD.status='in_progress' AND NEW.status IN ('done','cancelled')) OR
            (OLD.status='done' AND NEW.status IN ('in_progress','cancelled'))) THEN
            RAISE EXCEPTION 'Invalid task transition' USING ERRCODE='23514', CONSTRAINT='tasks_transition';
        END IF;
        NEW.closed_at=task_closure_time(OLD.status,NEW.status,OLD.closed_at,clock_timestamp());
    END IF;
    RETURN NEW;
END $$;
