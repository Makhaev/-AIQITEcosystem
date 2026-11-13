-- Preserve references from lessons, reports and audit history when removing access.
ALTER TABLE users ADD COLUMN deleted_at timestamptz;
ALTER TABLE users ADD CONSTRAINT users_deleted_blocked CHECK (deleted_at IS NULL OR blocked);
