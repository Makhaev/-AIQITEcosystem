ALTER TABLE family_reports
 ADD COLUMN snapshot jsonb,
 ADD COLUMN next_focus text NOT NULL DEFAULT '',
 ADD COLUMN reviewer_id bigint REFERENCES users(id),
 ADD COLUMN created_at timestamptz,
 ADD COLUMN updated_by bigint REFERENCES users(id),
 ADD COLUMN published_at timestamptz,
 ADD COLUMN withdrawn_at timestamptz;
-- Recover known historical dates; do not invent a creation time for legacy rows.
UPDATE family_reports r SET created_at=(SELECT min(h.created_at) FROM family_report_history h WHERE h.report_id=r.id),
 updated_by=COALESCE((SELECT h.actor_id FROM family_report_history h WHERE h.report_id=r.id ORDER BY revision DESC LIMIT 1),created_by);
ALTER TABLE family_reports ALTER COLUMN created_at SET DEFAULT clock_timestamp();
