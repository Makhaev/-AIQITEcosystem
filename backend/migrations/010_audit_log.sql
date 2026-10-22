ALTER TABLE audit_logs ADD COLUMN changed_fields text[] NOT NULL DEFAULT '{}';
CREATE INDEX audit_logs_chronology ON audit_logs(occurred_at DESC,id DESC);
CREATE INDEX audit_logs_actor_chronology ON audit_logs(actor_id,occurred_at DESC,id DESC);

CREATE FUNCTION guard_audit_log() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Audit log is append-only'
      USING ERRCODE='23514', CONSTRAINT='audit_logs_append_only';
END $$;
CREATE TRIGGER audit_logs_append_only BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
