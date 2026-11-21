-- +goose Up
CREATE TABLE garmin_connections (
    user_id           uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    account_handle    text NOT NULL,
    session_encrypted bytea NOT NULL,
    lease             uuid,
    lease_until       timestamptz,
    window_start      timestamptz,
    watermark         timestamptz,
    cursor_day        timestamptz,
    next_run          timestamptz,
    last_sync         timestamptz,
    page_offset       integer NOT NULL DEFAULT 0,
    history_complete  boolean NOT NULL DEFAULT false,
    history_rides     integer NOT NULL DEFAULT 0,
    summary_only      integer NOT NULL DEFAULT 0,
    skipped           integer NOT NULL DEFAULT 0,
    failures          integer NOT NULL DEFAULT 0,
    last_error        text NOT NULL DEFAULT '',
    needs_key         boolean NOT NULL DEFAULT false
);

CREATE INDEX garmin_connections_due ON garmin_connections (next_run)
    WHERE next_run IS NOT NULL AND NOT needs_key;

-- +goose Down
DROP TABLE garmin_connections;
