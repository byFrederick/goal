-- An item's repositories (ADR 0041 decision 4), keyed by normalized host/owner/name.

-- name: InsertItemRepository :exec
-- One repository the item starts with, written with the item (fileItem).
INSERT INTO work_item_repositories (item_id, repo, joined, joined_at, ordinal)
VALUES (sqlc.arg(item_id), sqlc.arg(repo), sqlc.arg(joined), sqlc.arg(joined_at), sqlc.arg(ordinal)::int);

-- name: ItemRepositories :many
-- First-joined order: those named at filing in the order named, then the rest by when they
-- joined, then by ordinal.
SELECT * FROM work_item_repositories WHERE item_id = $1
ORDER BY (joined <> 'filing'), joined_at, ordinal;
