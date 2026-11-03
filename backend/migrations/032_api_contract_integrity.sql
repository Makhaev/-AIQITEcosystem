-- Additive integrity/performance changes only. Existing rows and immutable
-- invitation, publication, participation and delivery histories are retained.
ALTER TABLE password_reset_tokens ADD CONSTRAINT password_reset_positive_lifetime CHECK(expires_at>created_at);
ALTER TABLE password_reset_rate_limits ADD CONSTRAINT password_reset_attempts_nonnegative CHECK(attempts>=0);
CREATE INDEX invitations_pending_expiry ON invitations(expires_at,id) WHERE used_at IS NULL AND revoked_at IS NULL;

ALTER TABLE family_reports ADD CONSTRAINT family_reports_revision_positive CHECK(revision>0);
ALTER TABLE family_reports ADD CONSTRAINT family_reports_publication_version CHECK(published_version IS NULL OR published_version BETWEEN 1 AND version);
ALTER TABLE family_report_editions ADD CONSTRAINT family_report_editions_version_positive CHECK(version>0);
ALTER TABLE family_report_editors ADD CONSTRAINT family_report_editors_version_positive CHECK(version>0);
ALTER TABLE family_report_decisions ADD CONSTRAINT family_report_decisions_version_positive CHECK(version>0);
CREATE INDEX family_report_decisions_version ON family_report_decisions(report_id,version,id);
CREATE INDEX family_report_notifications_recipient ON family_report_notifications(user_id,created_at DESC,report_id DESC);
CREATE INDEX family_reports_review_queue ON family_reports(reviewer_id,state,id);
ALTER TABLE family_requests ADD CONSTRAINT family_requests_revision_positive CHECK(revision>0);
CREATE INDEX family_requests_status_queue ON family_requests(status,id);

ALTER TABLE event_registrations ADD CONSTRAINT event_registration_revision_positive CHECK(revision>0);
ALTER TABLE event_registration_history ADD CONSTRAINT event_history_revision_positive CHECK(revision>0);
ALTER TABLE event_registration_history ADD CONSTRAINT event_history_valid_status CHECK(status IN ('requested','confirmed','waitlisted','cancelled','attended','no_show'));
CREATE INDEX event_registration_student ON event_registrations(student_id,event_id,id);
CREATE INDEX event_documents_registration ON event_documents(registration_id,id);

CREATE FUNCTION guard_event_registration_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.event_id,NEW.student_id,NEW.created_by,NEW.created_at) IS DISTINCT FROM
    (OLD.event_id,OLD.student_id,OLD.created_by,OLD.created_at) OR NEW.revision<>OLD.revision+1 THEN
  RAISE EXCEPTION 'registration identity is immutable; revision must advance' USING ERRCODE='23514';
 END IF;
 IF NOT (
  (OLD.status IN ('requested','waitlisted') AND NEW.status IN ('confirmed','cancelled')) OR
  (OLD.status='confirmed' AND NEW.status IN ('cancelled','attended','no_show')) OR
  (OLD.status='attended' AND NEW.status='no_show') OR
  (OLD.status='no_show' AND NEW.status='attended')
 ) THEN RAISE EXCEPTION 'invalid registration transition' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER event_registration_transition BEFORE UPDATE ON event_registrations FOR EACH ROW EXECUTE FUNCTION guard_event_registration_update();

ALTER TABLE notification_deliveries ADD CONSTRAINT notification_delivery_revision_positive CHECK(revision>0);
ALTER TABLE notification_deliveries ADD CONSTRAINT notification_delivery_cycle_attempts CHECK(cycle_attempts<=attempts);
ALTER TABLE notifications ADD CONSTRAINT notification_event_version_nonempty CHECK(length(btrim(event_version))>0);
