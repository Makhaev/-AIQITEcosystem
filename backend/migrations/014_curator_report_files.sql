CREATE TABLE curator_report_files (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    report_id bigint NOT NULL REFERENCES curator_reports(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
    content bytea NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 10485760),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX curator_report_files_report_idx ON curator_report_files(report_id,id);

CREATE FUNCTION guard_published_curator_report_file() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    publication timestamptz;
BEGIN
    IF TG_OP IN ('UPDATE','DELETE') THEN
        SELECT published_at INTO publication FROM curator_reports WHERE id=OLD.report_id FOR UPDATE;
        IF publication IS NOT NULL THEN
            RAISE EXCEPTION 'Published report attachments are immutable' USING ERRCODE='23514',CONSTRAINT='curator_report_file_published';
        END IF;
    END IF;
    IF TG_OP IN ('INSERT','UPDATE') THEN
        SELECT published_at INTO publication FROM curator_reports WHERE id=NEW.report_id FOR UPDATE;
        IF publication IS NOT NULL THEN
            RAISE EXCEPTION 'Published report attachments are immutable' USING ERRCODE='23514',CONSTRAINT='curator_report_file_published';
        END IF;
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER curator_report_files_immutable BEFORE INSERT OR UPDATE OR DELETE ON curator_report_files
    FOR EACH ROW EXECUTE FUNCTION guard_published_curator_report_file();
