-- +goose Up
-- An assembly's integration branch, the branch its parts build on when the line's
-- flow.assemblies.integration.mode is branch, and integration_land_at, the station where the
-- finished assembly lands to merge it (ports.Store SetIntegrationBranch, which sets both once;
-- closeAssembly moves such an assembly there, queued, instead of closing it done). NULL on every
-- other item, and on assemblies built on the default branch.
ALTER TABLE work_items ADD COLUMN integration_branch text, ADD COLUMN integration_land_at text;
