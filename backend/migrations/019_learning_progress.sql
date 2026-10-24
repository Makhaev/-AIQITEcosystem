CREATE TABLE program_plan_assignments (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 group_id bigint NOT NULL REFERENCES groups(id),
 version_id bigint NOT NULL REFERENCES program_versions(id),
 assigned_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 ended_at timestamptz
);
CREATE UNIQUE INDEX program_plan_current ON program_plan_assignments(group_id) WHERE ended_at IS NULL;
INSERT INTO program_plan_assignments(group_id,version_id) SELECT id,program_version_id FROM groups;
CREATE FUNCTION assign_program_plan() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' OR NEW.program_version_id IS DISTINCT FROM OLD.program_version_id THEN
  UPDATE program_plan_assignments SET ended_at=clock_timestamp() WHERE group_id=NEW.id AND ended_at IS NULL;
  INSERT INTO program_plan_assignments(group_id,version_id) VALUES(NEW.id,NEW.program_version_id);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER groups_program_plan AFTER INSERT OR UPDATE OF program_version_id ON groups FOR EACH ROW EXECUTE FUNCTION assign_program_plan();
ALTER TABLE lessons ADD COLUMN plan_assignment_id bigint REFERENCES program_plan_assignments(id);
UPDATE lessons l SET plan_assignment_id=p.id FROM program_plan_assignments p WHERE p.group_id=l.group_id AND p.version_id=l.program_version_id;
CREATE FUNCTION assign_lesson_plan() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' OR NEW.group_id IS DISTINCT FROM OLD.group_id OR NEW.program_version_id IS DISTINCT FROM OLD.program_version_id THEN
  SELECT id INTO NEW.plan_assignment_id FROM program_plan_assignments WHERE group_id=NEW.group_id AND version_id=NEW.program_version_id AND ended_at IS NULL;
 ELSIF NEW.plan_assignment_id IS DISTINCT FROM OLD.plan_assignment_id THEN
  RAISE EXCEPTION 'Plan assignment cannot be replaced directly';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER lessons_z_plan_assignment BEFORE INSERT OR UPDATE ON lessons FOR EACH ROW EXECUTE FUNCTION assign_lesson_plan();
CREATE TABLE lesson_reports (
 lesson_id bigint NOT NULL REFERENCES lessons(id),
 revision integer NOT NULL CHECK(revision>0),
 state text NOT NULL CHECK(state IN ('draft','submitted')),
 outcome text NOT NULL,
 plan_result text NOT NULL CHECK(plan_result IN ('complete','partial','not_completed')),
 comment text NOT NULL,
 confirm_element boolean NOT NULL DEFAULT false,
 plan_assignment_id bigint REFERENCES program_plan_assignments(id),
 template_key text NOT NULL,
 actor_id bigint NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 reason text NOT NULL,
 PRIMARY KEY(lesson_id,revision),
 CHECK(NOT confirm_element OR plan_result<>'not_completed' AND plan_assignment_id IS NOT NULL AND template_key<>''),
 CHECK(plan_result<>'partial' OR length(trim(comment))>0),
 CHECK(state<>'submitted' OR length(trim(outcome))>0)
);
CREATE TRIGGER lesson_reports_immutable BEFORE UPDATE OR DELETE ON lesson_reports FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
