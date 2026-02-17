-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, email, hashed_password)
VALUES (
    gen_random_uuid(),
    NOW(),
    NOW(),
    $1,
    $2
)
RETURNING *;

-- name: GetUser :one
SELECT *
FROM users
WHERE email = $1;

-- name: UpdateUserEmail :exec
UPDATE users
SET email = $2
WHERE id = $1;

-- name: UpdateUserPassword :exec
UPDATE users
SET hashed_password = $2
WHERE id = $1;


-- name: UpdateUserChirpyRed :exec
UPDATE users
SET is_chirpy_red = TRUE
WHERE id = $1;