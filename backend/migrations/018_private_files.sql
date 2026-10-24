-- Private educational attachments. Bytes and metadata commit atomically and are
-- included in the existing PostgreSQL backup. No public filesystem/URL exists.
CREATE TABLE private_files (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 student_id bigint NOT NULL REFERENCES students(id),
 name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
 media_type text NOT NULL,
 data bytea NOT NULL CHECK (octet_length(data) BETWEEN 1 AND 10485760),
 sha256 text NOT NULL CHECK (length(sha256)=64),
 created_by bigint NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 revoked_at timestamptz
);
CREATE INDEX private_files_student ON private_files(student_id,id DESC);
CREATE TABLE private_file_grants (
 file_id bigint NOT NULL REFERENCES private_files(id),
 user_id bigint NOT NULL REFERENCES users(id),
 can_manage boolean NOT NULL DEFAULT false,
 PRIMARY KEY(file_id,user_id)
);
CREATE FUNCTION guard_private_file() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'File history is retained'; END IF;
 IF (NEW.student_id,NEW.name,NEW.media_type,NEW.data,NEW.sha256,NEW.created_by,NEW.created_at)
 IS DISTINCT FROM (OLD.student_id,OLD.name,OLD.media_type,OLD.data,OLD.sha256,OLD.created_by,OLD.created_at)
 OR OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at THEN
  RAISE EXCEPTION 'File content and revocation are immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER private_file_immutable BEFORE UPDATE OR DELETE ON private_files FOR EACH ROW EXECUTE FUNCTION guard_private_file();
