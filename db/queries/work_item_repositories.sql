-- An item's repositories (ADR 0041 decision 4), keyed by normalized host/owner/name.

-- name: InsertItemRepository :exec
-- One repository the item starts with, written with the item (fileItem); a part's has its
-- assembly's base commit for the same repository.
INSERT INTO work_item_repositories (item_id, repo, joined, joined_at, ordinal, base_commit)
VALUES (sqlc.arg(item_id), sqlc.arg(repo), sqlc.arg(joined), sqlc.arg(joined_at), sqlc.arg(ordinal)::int,
  sqlc.narg(base_commit)::text);

-- name: ItemRepositories :many
-- First-joined order: those named at filing in the order named, then the rest by when they
-- joined, then by ordinal.
SELECT * FROM work_item_repositories WHERE item_id = $1
ORDER BY (joined <> 'filing'), joined_at, ordinal;

-- name: JoinItemRepository :exec
-- One repository joining an item after it was filed, with the next ordinal the caller computed;
-- a repository the item already has stays as it is.
INSERT INTO work_item_repositories (item_id, repo, joined, joined_at, ordinal)
VALUES (sqlc.arg(item_id), sqlc.arg(repo), sqlc.arg(joined), sqlc.arg(joined_at), sqlc.arg(ordinal)::int)
ON CONFLICT (item_id, repo) DO NOTHING;

-- name: OpenItemRepositories :many
-- The repositories the factory's open items use, by repository then item number: publish
-- refuses a version that drops the entry one of them needs (ADR 0041 decision 4).
SELECT r.repo, w.id AS item_id, w.number
FROM work_item_repositories r
JOIN work_items w ON w.org_id = r.org_id AND w.id = r.item_id
WHERE w.factory_id = $1 AND w.state NOT IN ('done', 'cancelled')
ORDER BY r.repo, w.number;

-- name: SetItemRepositoryBaseCommit :exec
-- The base commit of one of the item's repositories (ADR 0041 decision 5), set by a transition;
-- a repository the item lacks changes nothing.
UPDATE work_item_repositories SET base_commit = sqlc.arg(base_commit)::text
WHERE item_id = sqlc.arg(item_id) AND repo = sqlc.arg(repo);
-- name: SetIntegrationBranchOfRepositories :exec
-- An assembly's integration branch in each of repos it has (ADR 0041 decision 5), set once: a
-- row that has one keeps it, and base_commit is left as it is.
UPDATE work_item_repositories SET integration_branch = sqlc.arg(integration_branch)::text
WHERE item_id = sqlc.arg(item_id) AND repo = ANY(sqlc.arg(repos)::text[]) AND integration_branch IS NULL;

-- name: DonePartRepositories :many
-- The repositories of an assembly's done parts, in plan order and each part's first-joined
-- order, repeats included: what its landing covers.
SELECT r.repo
FROM work_items w
JOIN work_item_repositories r ON r.org_id = w.org_id AND r.item_id = w.id
WHERE w.parent_id = sqlc.arg(parent_id)::uuid AND w.part_key IS NOT NULL AND w.state = 'done'
ORDER BY w.number, (r.joined <> 'filing'), r.joined_at, r.ordinal;
