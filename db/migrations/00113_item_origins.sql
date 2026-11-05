-- +goose Up
-- Where each item came from and who filed it (ADR 0040 decisions 2 and 24): origin is person,
-- plan, trigger or follow-up; via is how a person filed it (web, cli or api, null otherwise);
-- created_by is the filer's actor ref (user:<id>, service:<key id>, agent:<name>,
-- system:engine); generation is 0 for people's and triggers' items and an assembly's for its
-- parts; untrusted marks work made from a webhook delivery, which a part inherits.
--
-- Expand only: source stays and goald still writes it, because old instances run during a
-- deploy. Those instances insert rows without the new columns, which take their defaults: a part
-- one of them inserts records origin person, not plan, until every instance is replaced.
--
-- The backfill gives plan to every row with a parent and reads origin and via from source for
-- the rest: people, cli and api are a person through web, cli and api; schedule, webhook and
-- github are a trigger. Rows without a source, the older ones, stay person with no via. Old rows
-- keep created_by null.
ALTER TABLE work_items
  ADD COLUMN origin     text NOT NULL DEFAULT 'person',
  ADD COLUMN via        text,
  ADD COLUMN created_by text,
  ADD COLUMN generation int NOT NULL DEFAULT 0,
  ADD COLUMN untrusted  bool NOT NULL DEFAULT false;

UPDATE work_items SET origin = 'plan' WHERE parent_id IS NOT NULL;
UPDATE work_items SET origin = 'trigger' WHERE parent_id IS NULL AND source IN ('schedule', 'webhook', 'github');
UPDATE work_items SET via = CASE source WHEN 'people' THEN 'web' WHEN 'cli' THEN 'cli' WHEN 'api' THEN 'api' END
  WHERE parent_id IS NULL AND source IN ('people', 'cli', 'api');

ALTER TABLE work_items
  ADD CONSTRAINT work_items_origin_check CHECK (origin IN ('person', 'plan', 'trigger', 'follow-up')) NOT VALID,
  ADD CONSTRAINT work_items_via_check CHECK (via IN ('web', 'cli', 'api')) NOT VALID,
  ADD CONSTRAINT work_items_generation_check CHECK (generation >= 0) NOT VALID;
ALTER TABLE work_items VALIDATE CONSTRAINT work_items_origin_check;
ALTER TABLE work_items VALIDATE CONSTRAINT work_items_via_check;
ALTER TABLE work_items VALIDATE CONSTRAINT work_items_generation_check;
