CREATE TABLE curator_reports (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    student_id bigint NOT NULL REFERENCES students(id),
    created_by bigint NOT NULL REFERENCES users(id),
    period_from date NOT NULL,
    period_to date NOT NULL CHECK (period_to>=period_from AND period_to-period_from<366),
    summary text NOT NULL CHECK (char_length(btrim(summary)) BETWEEN 1 AND 10000),
    recommendations text NOT NULL DEFAULT '' CHECK (char_length(recommendations)<=5000),
    goals text NOT NULL DEFAULT '' CHECK (char_length(goals)<=5000),
    snapshot jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(snapshot)='object'),
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (published_at IS NULL OR snapshot<>'{}'::jsonb)
);
CREATE INDEX curator_reports_student ON curator_reports(student_id,created_at DESC,id DESC);
CREATE TABLE curator_report_recipients (
    report_id bigint NOT NULL REFERENCES curator_reports(id),
    parent_id bigint NOT NULL REFERENCES parents(id),
    PRIMARY KEY(report_id,parent_id)
);
CREATE INDEX curator_report_recipients_parent ON curator_report_recipients(parent_id,report_id);
CREATE TABLE curator_report_feedback (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    report_id bigint NOT NULL REFERENCES curator_reports(id),
    parent_id bigint NOT NULL REFERENCES parents(id),
    message text NOT NULL CHECK (char_length(btrim(message)) BETWEEN 1 AND 3000),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(report_id,parent_id) REFERENCES curator_report_recipients(report_id,parent_id)
);
CREATE INDEX curator_report_feedback_report ON curator_report_feedback(report_id,id);
CREATE FUNCTION guard_published_curator_report() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.published_at IS NOT NULL AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'Published report is immutable' USING ERRCODE='23514',CONSTRAINT='curator_report_published';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER curator_report_immutable BEFORE UPDATE ON curator_reports FOR EACH ROW EXECUTE FUNCTION guard_published_curator_report();
