CREATE TABLE IF NOT EXISTS notifications (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      TEXT NOT NULL,
    event_id     TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    channel      TEXT NOT NULL,
    status       TEXT NOT NULL,
    payload      TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(event_id, channel)
);
CREATE INDEX IF NOT EXISTS idx_notifications_user_id ON notifications(user_id);
