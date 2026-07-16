-- name: CreateNotification :one
INSERT INTO notifications (user_id, company_id, type, title, body, data)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListNotificationsByUser :many
SELECT * FROM notifications
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountUnreadNotifications :one
SELECT COUNT(*) FROM notifications
WHERE user_id = $1 AND read_at IS NULL;

-- name: MarkNotificationRead :exec
UPDATE notifications SET read_at = NOW()
WHERE id = $1 AND user_id = $2 AND read_at IS NULL;

-- name: MarkAllNotificationsRead :exec
UPDATE notifications SET read_at = NOW()
WHERE user_id = $1 AND read_at IS NULL;

-- name: GetNotificationPreferences :many
SELECT * FROM notification_preferences
WHERE user_id = $1;

-- name: UpsertNotificationPreference :one
INSERT INTO notification_preferences (user_id, event_type, email, in_app, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (user_id, event_type) DO UPDATE
SET email      = EXCLUDED.email,
    in_app     = EXCLUDED.in_app,
    updated_at = NOW()
RETURNING *;

-- name: GetNotificationPreference :one
SELECT * FROM notification_preferences
WHERE user_id = $1 AND event_type = $2;
