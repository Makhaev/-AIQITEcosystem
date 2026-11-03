ALTER TABLE family_reports
 ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK(version>0),
 ADD COLUMN state text NOT NULL DEFAULT 'draft' CHECK(state IN ('draft','review','approved','published','withdrawn')),
 ADD COLUMN published_version integer,
 ADD COLUMN approved_by bigint REFERENCES users(id),
 ADD COLUMN approved_at timestamptz;
UPDATE family_reports SET state='published',published_version=1 WHERE published;

CREATE TABLE family_report_editors (
 report_id bigint NOT NULL REFERENCES family_reports(id), version integer NOT NULL,
 user_id bigint NOT NULL REFERENCES users(id), PRIMARY KEY(report_id,version,user_id)
);
INSERT INTO family_report_editors SELECT id,1,created_by FROM family_reports;
-- Historical mutations did not identify editors separately: conservatively
-- exclude all recorded participants from approving the migrated version.
INSERT INTO family_report_editors SELECT DISTINCT report_id,1,actor_id FROM family_report_history ON CONFLICT DO NOTHING;

CREATE TABLE family_report_editions (
 report_id bigint NOT NULL REFERENCES family_reports(id), version integer NOT NULL,
 content jsonb NOT NULL, PRIMARY KEY(report_id,version)
);
CREATE TABLE family_report_edition_files (
 report_id bigint NOT NULL, version integer NOT NULL, file_id bigint NOT NULL REFERENCES private_files(id),
 PRIMARY KEY(report_id,version,file_id), FOREIGN KEY(report_id,version) REFERENCES family_report_editions(report_id,version)
);
CREATE TABLE family_report_publications (
 report_id bigint NOT NULL, version integer NOT NULL, published_at timestamptz,
 published_by bigint REFERENCES users(id), withdrawn_at timestamptz,
 PRIMARY KEY(report_id,version), FOREIGN KEY(report_id,version) REFERENCES family_report_editions(report_id,version)
);
CREATE TABLE family_report_decisions (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 report_id bigint NOT NULL REFERENCES family_reports(id), version integer NOT NULL,
 action text NOT NULL, reason text NOT NULL DEFAULT '', actor_id bigint NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE family_report_notifications (
 report_id bigint NOT NULL, version integer NOT NULL, user_id bigint NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(report_id,version,user_id), FOREIGN KEY(report_id,version) REFERENCES family_report_publications(report_id,version)
);

INSERT INTO family_report_editions(report_id,version,content)
 SELECT r.id,1,to_jsonb(r)||jsonb_build_object('author_id',r.created_by,'author_name',u.name,'reviewer_name',COALESCE(rv.name,''),'files','[]'::jsonb,'legacy',true)
 FROM family_reports r JOIN users u ON u.id=r.created_by LEFT JOIN users rv ON rv.id=r.reviewer_id WHERE r.published;
INSERT INTO family_report_edition_files SELECT f.report_id,1,f.file_id FROM family_report_files f JOIN family_reports r ON r.id=f.report_id WHERE r.published;
INSERT INTO family_report_publications(report_id,version,published_at) SELECT id,1,published_at FROM family_reports WHERE published;

CREATE TRIGGER family_report_editions_immutable BEFORE UPDATE OR DELETE ON family_report_editions FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
CREATE TRIGGER family_report_edition_files_immutable BEFORE UPDATE OR DELETE ON family_report_edition_files FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
CREATE TRIGGER family_report_decisions_immutable BEFORE UPDATE OR DELETE ON family_report_decisions FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
CREATE TRIGGER family_report_editors_immutable BEFORE UPDATE OR DELETE ON family_report_editors FOR EACH ROW EXECUTE FUNCTION guard_audit_log();

CREATE FUNCTION guard_family_report_content() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state<>'draft' AND NEW.version=OLD.version AND
   (NEW.title,NEW.body,NEW.snapshot,NEW.next_focus,NEW.reviewer_id,NEW.created_by)
   IS DISTINCT FROM (OLD.title,OLD.body,OLD.snapshot,OLD.next_focus,OLD.reviewer_id,OLD.created_by) THEN
  RAISE EXCEPTION 'Only a draft may change content' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER family_report_content_guard BEFORE UPDATE ON family_reports FOR EACH ROW EXECUTE FUNCTION guard_family_report_content();

CREATE FUNCTION guard_family_report_files() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' AND EXISTS(SELECT 1 FROM family_reports WHERE id=OLD.report_id AND state<>'draft') THEN
  RAISE EXCEPTION 'Only draft attachments may change' USING ERRCODE='23514';
 END IF;
 IF TG_OP<>'DELETE' AND EXISTS(SELECT 1 FROM family_reports WHERE id=NEW.report_id AND state<>'draft') THEN
  RAISE EXCEPTION 'Only draft attachments may change' USING ERRCODE='23514';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER family_report_files_guard BEFORE INSERT OR UPDATE OR DELETE ON family_report_files FOR EACH ROW EXECUTE FUNCTION guard_family_report_files();
