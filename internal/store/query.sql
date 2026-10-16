-- name: InsertMessage :exec
INSERT INTO messages (
    message_id, seq, type, sender_id, sender_name,
    timestamp, content, image_data,
    reply_to, reply_to_sender_id, reply_to_preview
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetMessageByID :one
SELECT * FROM messages WHERE message_id = ?;

-- name: GetRecentMessages :many
SELECT * FROM messages
ORDER BY timestamp DESC
LIMIT ?;

-- name: GetAllMessages :many
SELECT * FROM messages ORDER BY timestamp ASC;

-- name: ClearHotWords :exec
DELETE FROM hot_words;

-- name: InsertHotWord :exec
INSERT INTO hot_words (word, category, created_at) VALUES (?, ?, ?);

-- name: GetRandomModifier :one
SELECT word FROM hot_words WHERE category = 'modifier' ORDER BY RANDOM() LIMIT 1;

-- name: GetRandomNoun :one
SELECT word FROM hot_words WHERE category = 'noun' ORDER BY RANDOM() LIMIT 1;

-- name: GetIdentity :one
SELECT * FROM identities WHERE client_id = ?;

-- name: UpsertIdentity :exec
INSERT INTO identities (client_id, user_id, name, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(client_id) DO UPDATE SET
    user_id    = excluded.user_id,
    name       = excluded.name,
    updated_at = excluded.updated_at;