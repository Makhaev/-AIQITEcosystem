ALTER TABLE tasks ADD COLUMN closed_at timestamptz;
-- Old closed tasks retain NULL: updated_at does not prove when they closed.
ALTER TABLE tasks ADD CONSTRAINT tasks_open_closed_at CHECK (status IN ('done','cancelled') OR closed_at IS NULL);

CREATE FUNCTION guard_task_lifecycle() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.status<>'todo' THEN
            RAISE EXCEPTION 'Tasks start in todo' USING ERRCODE='23514', CONSTRAINT='tasks_initial_status';
        END IF;
        NEW.closed_at=NULL;
    ELSIF NEW.status IS DISTINCT FROM OLD.status THEN
        IF NOT ((OLD.status='todo' AND NEW.status IN ('in_progress','cancelled')) OR
                (OLD.status='in_progress' AND NEW.status IN ('done','cancelled')) OR
                (OLD.status='done' AND NEW.status IN ('in_progress','cancelled'))) THEN
            RAISE EXCEPTION 'Invalid task transition' USING ERRCODE='23514', CONSTRAINT='tasks_transition';
        END IF;
        IF NEW.status='in_progress' THEN
            NEW.closed_at=NULL;
        ELSIF OLD.status IN ('todo','in_progress') THEN
            NEW.closed_at=clock_timestamp();
        ELSE
            NEW.closed_at=OLD.closed_at;
        END IF;
    ELSE
        NEW.closed_at=OLD.closed_at;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER tasks_lifecycle BEFORE INSERT OR UPDATE ON tasks
    FOR EACH ROW EXECUTE FUNCTION guard_task_lifecycle();
