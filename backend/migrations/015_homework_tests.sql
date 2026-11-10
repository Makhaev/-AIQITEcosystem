CREATE TABLE homework_tests (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title text NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 200),
    instructions text NOT NULL DEFAULT '' CHECK (char_length(instructions)<=5000),
    questions jsonb NOT NULL CHECK (jsonb_typeof(questions)='array' AND jsonb_array_length(questions) BETWEEN 1 AND 30),
    created_by bigint NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE homework_test_assignments (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    test_id bigint NOT NULL REFERENCES homework_tests(id),
    student_id bigint NOT NULL REFERENCES students(id),
    due_at timestamptz,
    answers jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(answers)='array'),
    correct_count integer NOT NULL DEFAULT 0 CHECK (correct_count>=0),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts>=0),
    completed_at timestamptz,
    last_attempt_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(test_id,student_id)
);
CREATE INDEX homework_test_assignments_student ON homework_test_assignments(student_id,id);
CREATE INDEX homework_tests_author ON homework_tests(created_by,id);
