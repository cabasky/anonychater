CREATE TABLE IF NOT EXISTS messages (
    message_id         TEXT    PRIMARY KEY,
    seq                INTEGER NOT NULL DEFAULT 0,
    type               INTEGER NOT NULL,
    sender_id          TEXT    NOT NULL,
    sender_name        TEXT    NOT NULL,
    timestamp          INTEGER NOT NULL,
    content            TEXT    NOT NULL DEFAULT '',
    image_data         BLOB,
    reply_to           TEXT    NOT NULL DEFAULT '',
    reply_to_sender_id TEXT    NOT NULL DEFAULT '',
    reply_to_preview   TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_messages_sender    ON messages(sender_id);

CREATE TABLE IF NOT EXISTS hot_words (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    word       TEXT NOT NULL,
    category   TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE(word, category)
);

CREATE TABLE IF NOT EXISTS identities (
    client_id  TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    name       TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_identities_user ON identities(user_id);