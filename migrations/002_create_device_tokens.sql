CREATE TABLE IF NOT EXISTS device_tokens (
    user_id      TEXT PRIMARY KEY,
    token        TEXT NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
