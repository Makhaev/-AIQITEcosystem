CREATE TABLE tasks (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title text NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 200),
    title_search text NOT NULL,
    description text NOT NULL CHECK (char_length(btrim(description)) BETWEEN 1 AND 5000),
    creator_id bigint NOT NULL REFERENCES users(id),
    assignee_id bigint NOT NULL REFERENCES users(id),
    due_at timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'todo' CHECK (status IN ('todo','in_progress','done','cancelled')),
    student_id bigint REFERENCES students(id),
    group_id bigint REFERENCES groups(id),
    lesson_id bigint REFERENCES lessons(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tasks_one_related_object CHECK (num_nonnulls(student_id,group_id,lesson_id)<=1)
);
CREATE INDEX tasks_assignee_due ON tasks(assignee_id,due_at,id);
CREATE INDEX tasks_status_due ON tasks(status,due_at,id);

CREATE FUNCTION guard_task_creator() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.creator_id<>OLD.creator_id THEN
        RAISE EXCEPTION 'Task creator is immutable' USING ERRCODE='23514', CONSTRAINT='tasks_creator_immutable';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER tasks_guard_creator BEFORE UPDATE OF creator_id ON tasks
    FOR EACH ROW EXECUTE FUNCTION guard_task_creator();
