CREATE TABLE notification_deliveries (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 kind text NOT NULL, object_id bigint NOT NULL, event_version text NOT NULL,
 user_id bigint NOT NULL REFERENCES users(id),
 channel text NOT NULL CHECK(channel IN ('in_app','email')),
 status text NOT NULL CHECK(status IN ('pending','processing','sent','failed')),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0),
 cycle_attempts integer NOT NULL DEFAULT 0 CHECK(cycle_attempts>=0),
 max_attempts integer NOT NULL DEFAULT 5 CHECK(max_attempts BETWEEN 1 AND 20),
 next_attempt_at timestamptz, lease_until timestamptz, claim_token uuid,
 revision bigint NOT NULL DEFAULT 1,
 idempotency_key uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 last_error text NOT NULL DEFAULT '', provider text NOT NULL DEFAULT '', provider_message_id text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), sent_at timestamptz, delivered_at timestamptz,
 delivery_evidence text NOT NULL DEFAULT '',
 UNIQUE(kind,object_id,event_version,user_id,channel),
 CHECK((status='processing')=(claim_token IS NOT NULL AND lease_until IS NOT NULL)),
 CHECK((status='pending')=(next_attempt_at IS NOT NULL)),
 CHECK(delivered_at IS NULL OR (status='sent' AND provider<>'' AND provider_message_id<>'' AND delivery_evidence<>''))
);
CREATE INDEX notification_deliveries_ready ON notification_deliveries(next_attempt_at,id) WHERE status='pending';
CREATE INDEX notification_deliveries_recovery ON notification_deliveries(lease_until,id) WHERE status='processing';
CREATE INDEX notification_deliveries_failed ON notification_deliveries(id DESC) WHERE status='failed';
CREATE UNIQUE INDEX notification_deliveries_provider_message ON notification_deliveries(provider,provider_message_id) WHERE provider_message_id<>'';

-- Keep context rows for live authorization, deduplicate each delivery channel.
INSERT INTO notification_deliveries(kind,object_id,event_version,user_id,channel,status,attempts,cycle_attempts,max_attempts,created_at,sent_at)
 SELECT kind,object_id,event_version,user_id,'in_app','sent',1,1,1,min(created_at),min(created_at)
 FROM notifications GROUP BY kind,object_id,event_version,user_id;
INSERT INTO notification_deliveries(kind,object_id,event_version,user_id,channel,status,attempts,cycle_attempts,next_attempt_at,last_error,created_at,sent_at,provider)
 SELECT kind,object_id,event_version,user_id,'email',
 CASE WHEN bool_or(email_state='sent') THEN 'sent' WHEN bool_or(email_state='pending') AND sum(attempts)<5 THEN 'pending' ELSE 'failed' END,
 sum(attempts)::int,LEAST(sum(attempts),5)::int,
 CASE WHEN NOT bool_or(email_state='sent') AND bool_or(email_state='pending') AND sum(attempts)<5 THEN min(next_attempt_at) END,
 CASE WHEN bool_or(email_state='sent') THEN '' WHEN NOT bool_or(email_state='pending') THEN 'recipient_unavailable' WHEN sum(attempts)>=5 THEN 'attempts_exhausted' ELSE '' END,
 min(created_at),min(sent_at),CASE WHEN bool_or(email_state='sent') THEN 'smtp' ELSE '' END
 FROM notifications WHERE email_state<>'external' AND kind NOT IN ('invitation','password_reset')
 GROUP BY kind,object_id,event_version,user_id;

CREATE TABLE notification_delivery_retries (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 delivery_id bigint NOT NULL REFERENCES notification_deliveries(id), actor_id bigint NOT NULL REFERENCES users(id),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 1000), previous_revision bigint NOT NULL,
 previous_attempts integer NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(delivery_id,previous_revision)
);
CREATE TRIGGER notification_delivery_retries_immutable BEFORE UPDATE OR DELETE ON notification_delivery_retries FOR EACH ROW EXECUTE FUNCTION guard_audit_log();
CREATE FUNCTION enqueue_notification_delivery() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO notification_deliveries(kind,object_id,event_version,user_id,channel,status,attempts,cycle_attempts,max_attempts,created_at,sent_at)
 VALUES(NEW.kind,NEW.object_id,NEW.event_version,NEW.user_id,'in_app','sent',1,1,1,NEW.created_at,NEW.created_at) ON CONFLICT DO NOTHING;
 -- One-time links keep their own issue/revoke/resend contracts; no token replay.
 IF NEW.kind NOT IN ('invitation','password_reset') THEN
  INSERT INTO notification_deliveries(kind,object_id,event_version,user_id,channel,status,next_attempt_at)
  VALUES(NEW.kind,NEW.object_id,NEW.event_version,NEW.user_id,'email','pending',clock_timestamp()) ON CONFLICT DO NOTHING;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER notifications_enqueue AFTER INSERT ON notifications FOR EACH ROW EXECUTE FUNCTION enqueue_notification_delivery();
ALTER TABLE notifications DROP COLUMN email_state, DROP COLUMN attempts, DROP COLUMN next_attempt_at, DROP COLUMN sent_at;
