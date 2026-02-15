-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (token, created_at, updated_at, user_id, expires_at, revoked_at)
VALUES (
    $1,
    NOW(),
    NOW(),
    $2,
    NOW() + INTERVAL '60 days',
    NULL
)
RETURNING *;


-- name: GetRefreshToken :one
SELECT *
FROM refresh_tokens
WHERE token = $1;


-- name: GetUserFromRefreshToken :one
SELECT *
FROM users u
JOIN refresh_tokens rt ON rt.user_id = u.id
WHERE rt.token = $1;

-- name: PostRevokeRefreshToken :exec
UPDATE refresh_tokens
SET
    updated_at = NOW(),
    revoked_at = NOW()
WHERE token = $1;