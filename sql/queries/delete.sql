-- name: DeleteUsers :exec
DELETE FROM users;

-- name: DeleteChirps :exec
DELETE FROM chirps;

-- name: DeleteTokens :exec
DELETE FROM refresh_tokens;