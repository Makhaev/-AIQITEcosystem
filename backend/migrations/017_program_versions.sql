ALTER TABLE user_roles DROP CONSTRAINT user_roles_role_check;
ALTER TABLE user_roles ADD CONSTRAINT user_roles_role_check
    CHECK (role IN ('admin','leader','teacher','curator','methodologist','education_leader'));

CREATE TABLE program_versions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    program_id bigint NOT NULL REFERENCES programs(id),
    number integer NOT NULL CHECK (number > 0),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','review','published','archived')),
    document jsonb NOT NULL CHECK (jsonb_typeof(document)='object'),
    revision integer NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_by bigint REFERENCES users(id),
    submitted_by bigint REFERENCES users(id),
    approved_by bigint REFERENCES users(id),
    source_version_id bigint REFERENCES program_versions(id),
    legacy_import boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    published_at timestamptz,
    UNIQUE(program_id,number),
    UNIQUE(id,program_id),
    CHECK (legacy_import OR created_by IS NOT NULL),
    CHECK (legacy_import OR status NOT IN ('published','archived') OR
           (approved_by IS NOT NULL AND submitted_by IS NOT NULL AND
            approved_by<>created_by AND approved_by<>submitted_by AND published_at IS NOT NULL))
);
CREATE INDEX program_versions_list ON program_versions(program_id,number DESC);
CREATE TABLE program_version_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    version_id bigint NOT NULL REFERENCES program_versions(id),
    actor_id bigint REFERENCES users(id),
    action text NOT NULL,
    comment text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX program_version_events_order ON program_version_events(version_id,id);

-- A migration snapshot is explicitly distinguished from educational approval.
-- Existing completed lessons' unknown historical content is not invented.
INSERT INTO program_versions(program_id,number,status,document,legacy_import)
SELECT p.id,1,CASE p.status WHEN 'active' THEN 'published' WHEN 'archived' THEN 'archived' ELSE 'draft' END,
 jsonb_build_object('name',p.name,'description',p.description,'sections',
   jsonb_build_array(jsonb_build_object('key','core','name','Раздел Core','modules',
     COALESCE((SELECT jsonb_agg(jsonb_build_object('key','core-module-'||m.id,
       'name',m.name,'description',m.description,'lessons','[]'::jsonb) ORDER BY m.position,m.id)
       FROM program_modules m WHERE m.program_id=p.id),'[]'::jsonb)))),true
FROM programs p;
INSERT INTO program_version_events(version_id,action,comment)
SELECT id,'imported','Снимок существующего Core. Утверждение и неизвестное историческое содержание не восстановлены.'
FROM program_versions;

CREATE FUNCTION guard_program_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        RAISE EXCEPTION 'Versions are retained' USING ERRCODE='23514', CONSTRAINT='program_versions_immutable';
    END IF;
    IF TG_OP='INSERT' THEN
        IF NEW.status<>'draft' OR NEW.legacy_import OR NEW.created_by IS NULL OR NEW.approved_by IS NOT NULL THEN
            RAISE EXCEPTION 'Create a draft first' USING ERRCODE='23514', CONSTRAINT='program_versions_transition';
        END IF;
        RETURN NEW;
    END IF;
    IF ROW(NEW.id,NEW.program_id,NEW.number,NEW.created_by,NEW.created_at,NEW.source_version_id,NEW.legacy_import)
       IS DISTINCT FROM ROW(OLD.id,OLD.program_id,OLD.number,OLD.created_by,OLD.created_at,OLD.source_version_id,OLD.legacy_import) THEN
        RAISE EXCEPTION 'Version identity is immutable' USING ERRCODE='23514', CONSTRAINT='program_versions_immutable';
    END IF;
    IF OLD.status IN ('published','archived') AND
       (NEW.document IS DISTINCT FROM OLD.document OR NEW.approved_by IS DISTINCT FROM OLD.approved_by OR
        NEW.submitted_by IS DISTINCT FROM OLD.submitted_by OR NEW.published_at IS DISTINCT FROM OLD.published_at) THEN
        RAISE EXCEPTION 'Published content is immutable' USING ERRCODE='23514', CONSTRAINT='program_versions_immutable';
    END IF;
    IF OLD.status<>'draft' AND NEW.document IS DISTINCT FROM OLD.document THEN
        RAISE EXCEPTION 'Only drafts are editable' USING ERRCODE='23514', CONSTRAINT='program_versions_immutable';
    END IF;
    IF NEW.status='published' AND EXISTS(SELECT 1 FROM program_version_events
       WHERE version_id=OLD.id AND actor_id=NEW.approved_by AND action IN ('created','edited','submit')) THEN
        RAISE EXCEPTION 'A contributor cannot approve their own work'
            USING ERRCODE='23514', CONSTRAINT='program_versions_self_approval';
    END IF;
    IF NOT ((OLD.status='draft' AND NEW.status IN ('draft','review')) OR
            (OLD.status='review' AND NEW.status IN ('draft','published')) OR
            (OLD.status='published' AND NEW.status='archived')) THEN
        RAISE EXCEPTION 'Invalid transition' USING ERRCODE='23514', CONSTRAINT='program_versions_transition';
    END IF;
    NEW.revision := OLD.revision+1;
    NEW.updated_at := clock_timestamp();
    RETURN NEW;
END $$;
CREATE TRIGGER program_versions_guard BEFORE INSERT OR UPDATE OR DELETE ON program_versions
FOR EACH ROW EXECUTE FUNCTION guard_program_version();

ALTER TABLE groups ADD COLUMN program_version_id bigint;
UPDATE groups g SET program_version_id=v.id FROM program_versions v WHERE v.program_id=g.program_id;
ALTER TABLE groups ALTER COLUMN program_version_id SET NOT NULL;
ALTER TABLE groups ADD CONSTRAINT groups_version_program FOREIGN KEY(program_version_id,program_id)
    REFERENCES program_versions(id,program_id);

CREATE TABLE group_program_transfers (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    group_id bigint NOT NULL REFERENCES groups(id),
    actor_id bigint NOT NULL REFERENCES users(id),
    previous_version_id bigint NOT NULL REFERENCES program_versions(id),
    target_version_id bigint NOT NULL REFERENCES program_versions(id),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 1000),
    previous_lessons jsonb NOT NULL,
    resulting_lessons jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX group_program_transfers_order ON group_program_transfers(group_id,id);

CREATE FUNCTION require_group_version() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target program_versions;
BEGIN
    IF TG_OP='UPDATE' THEN
        IF NEW.program_id=OLD.program_id AND NEW.program_version_id=OLD.program_version_id THEN RETURN NEW; END IF;
        IF current_setting('aiq.program_transfer',true) IS DISTINCT FROM OLD.id::text THEN
            RAISE EXCEPTION 'Use explicit program transfer' USING ERRCODE='23514', CONSTRAINT='groups_version_transfer_required';
        END IF;
    END IF;
    IF NEW.program_version_id IS NULL THEN
        SELECT id INTO NEW.program_version_id FROM program_versions
        WHERE program_id=NEW.program_id AND status='published' ORDER BY number DESC LIMIT 1;
    END IF;
    SELECT * INTO target FROM program_versions WHERE id=NEW.program_version_id FOR SHARE;
    IF NOT FOUND OR target.program_id<>NEW.program_id OR target.status<>'published' THEN
        RAISE EXCEPTION 'Published version required' USING ERRCODE='23514', CONSTRAINT='groups_published_version';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER groups_version BEFORE INSERT OR UPDATE OF program_id,program_version_id ON groups
FOR EACH ROW EXECUTE FUNCTION require_group_version();

CREATE OR REPLACE FUNCTION guard_group_changes() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.capacity < OLD.capacity AND NEW.capacity <
       (SELECT count(*) FROM group_enrollments WHERE group_id=OLD.id AND ends_on IS NULL) THEN
        RAISE EXCEPTION 'Capacity is below current enrollment count'
            USING ERRCODE='23514', CONSTRAINT='groups_capacity_exceeded';
    END IF;
    IF NEW.program_id<>OLD.program_id AND
       current_setting('aiq.program_transfer',true) IS DISTINCT FROM OLD.id::text AND
       (EXISTS(SELECT 1 FROM group_enrollments WHERE group_id=OLD.id) OR
        EXISTS(SELECT 1 FROM lessons WHERE group_id=OLD.id)) THEN
        RAISE EXCEPTION 'A group with history cannot change program'
            USING ERRCODE='23514', CONSTRAINT='groups_program_has_history';
    END IF;
    RETURN NEW;
END $$;

ALTER TABLE lessons ADD COLUMN program_version_id bigint;
ALTER TABLE lessons ADD COLUMN version_module_key text NOT NULL DEFAULT '';
ALTER TABLE lessons ADD COLUMN template_key text NOT NULL DEFAULT '';
ALTER TABLE lessons ADD COLUMN plan_snapshot jsonb NOT NULL DEFAULT '{}';
UPDATE lessons l SET program_version_id=g.program_version_id,
    plan_snapshot=jsonb_build_object('origin',CASE WHEN l.status='completed' THEN 'legacy_unknown' ELSE 'core_import' END,
                                    'topic',l.topic)
FROM groups g WHERE g.id=l.group_id;
ALTER TABLE lessons ALTER COLUMN program_version_id SET NOT NULL;
ALTER TABLE lessons ADD CONSTRAINT lessons_version_program FOREIGN KEY(program_version_id,program_id)
    REFERENCES program_versions(id,program_id);
ALTER TABLE lessons DROP CONSTRAINT lessons_group_program;
ALTER TABLE lessons ADD CONSTRAINT lessons_group_id_fkey FOREIGN KEY(group_id) REFERENCES groups(id);

CREATE OR REPLACE FUNCTION derive_lesson_program() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE group_program bigint; group_version bigint;
BEGIN
    SELECT program_id,program_version_id INTO group_program,group_version FROM groups WHERE id=NEW.group_id FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Group does not exist' USING ERRCODE='23503', CONSTRAINT='lessons_group_program';
    END IF;
    IF TG_OP='UPDATE' AND NEW.group_id=OLD.group_id AND
       current_setting('aiq.program_transfer',true) IS DISTINCT FROM OLD.group_id::text THEN
        IF NEW.program_id IS DISTINCT FROM OLD.program_id OR NEW.program_version_id IS DISTINCT FROM OLD.program_version_id THEN
            RAISE EXCEPTION 'Explicit transfer required' USING ERRCODE='23514', CONSTRAINT='lessons_program_matches_group';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.program_id IS NOT NULL AND NEW.program_id<>group_program THEN
        RAISE EXCEPTION 'Lesson program must match group' USING ERRCODE='23514', CONSTRAINT='lessons_program_matches_group';
    END IF;
    IF NEW.program_version_id IS NOT NULL AND NEW.program_version_id<>group_version THEN
        RAISE EXCEPTION 'Lesson version must match group' USING ERRCODE='23514', CONSTRAINT='lessons_version_matches_group';
    END IF;
    NEW.program_id := group_program;
    NEW.program_version_id := group_version;
    RETURN NEW;
END $$;
DROP TRIGGER lessons_derive_program ON lessons;
CREATE TRIGGER lessons_derive_program BEFORE INSERT OR UPDATE OF group_id,program_id,program_version_id,module_id ON lessons
FOR EACH ROW EXECUTE FUNCTION derive_lesson_program();

CREATE FUNCTION capture_lesson_plan() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v program_versions; section jsonb; module jsonb; template jsonb; found_module boolean := false;
BEGIN
    IF TG_OP='UPDATE' THEN
        IF ROW(NEW.group_id,NEW.program_version_id,NEW.version_module_key,NEW.template_key)
           IS NOT DISTINCT FROM ROW(OLD.group_id,OLD.program_version_id,OLD.version_module_key,OLD.template_key) THEN
            IF NEW.plan_snapshot IS DISTINCT FROM OLD.plan_snapshot THEN
                RAISE EXCEPTION 'Plan snapshot is immutable' USING ERRCODE='23514', CONSTRAINT='lessons_plan_immutable';
            END IF;
            RETURN NEW;
        END IF;
        IF OLD.status<>'scheduled' OR OLD.starts_at<=clock_timestamp() OR OLD.snapshot_state<>'not_captured' THEN
            RAISE EXCEPTION 'Historical plan is immutable' USING ERRCODE='23514', CONSTRAINT='lessons_plan_immutable';
        END IF;
    END IF;
    SELECT * INTO v FROM program_versions WHERE id=NEW.program_version_id;
    IF NOT FOUND OR v.status NOT IN ('published','archived') THEN
        RAISE EXCEPTION 'Published plan required' USING ERRCODE='23514', CONSTRAINT='lessons_published_version';
    END IF;
    NEW.plan_snapshot := jsonb_build_object('origin','version','program_name',v.document->>'name',
                                           'version_id',v.id,'version_number',v.number);
    IF NEW.version_module_key='' AND NEW.template_key='' THEN RETURN NEW; END IF;
    FOR section IN SELECT value FROM jsonb_array_elements(v.document->'sections') LOOP
        FOR module IN SELECT value FROM jsonb_array_elements(section->'modules') LOOP
            IF module->>'key'=NEW.version_module_key THEN
                found_module := true;
                NEW.plan_snapshot := NEW.plan_snapshot || jsonb_build_object('section_name',section->>'name','module_name',module->>'name');
                IF NEW.template_key='' THEN RETURN NEW; END IF;
                FOR template IN SELECT value FROM jsonb_array_elements(module->'lessons') LOOP
                    IF template->>'key'=NEW.template_key THEN
                        NEW.plan_snapshot := NEW.plan_snapshot || jsonb_build_object('template',template);
                        NEW.topic := template->>'topic';
                        NEW.topic_search := lower(NEW.topic);
                        RETURN NEW;
                    END IF;
                END LOOP;
            END IF;
        END LOOP;
    END LOOP;
    RAISE EXCEPTION 'Module/template does not belong to lesson version'
        USING ERRCODE='23514', CONSTRAINT='lessons_version_content';
END $$;
CREATE TRIGGER lessons_m2_plan BEFORE INSERT OR UPDATE ON lessons FOR EACH ROW EXECUTE FUNCTION capture_lesson_plan();

CREATE FUNCTION retain_curriculum_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Curriculum history is append only' USING ERRCODE='23514', CONSTRAINT='curriculum_history_immutable';
END $$;
CREATE TRIGGER program_version_events_retain BEFORE UPDATE OR DELETE ON program_version_events
FOR EACH ROW EXECUTE FUNCTION retain_curriculum_history();
CREATE TRIGGER group_program_transfers_retain BEFORE UPDATE OR DELETE ON group_program_transfers
FOR EACH ROW EXECUTE FUNCTION retain_curriculum_history();
