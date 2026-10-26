-- +goose Up
-- Phase 2.2 A7: slot caps. organizations.max_slots caps the agents running at once across an
-- organization's factories, factories.max_slots those of one factory; NULL is no cap. ClaimNext
-- enforces both in its transaction, under a per-organization advisory lock (A §4.4). Per-factory
-- caps may add up to more than the organization's: factories then wait for each other at peak.
-- Adding a nullable column without a default changes only the catalog. goal_app's table-level
-- grants (00004, 00005) cover the new columns, and goal_orgs()/goal_org_by_ref() return
-- SETOF organizations, so they carry max_slots without being replaced.
ALTER TABLE organizations ADD COLUMN max_slots integer
  CONSTRAINT organizations_max_slots_check CHECK (max_slots BETWEEN 1 AND 1000);
ALTER TABLE factories ADD COLUMN max_slots integer
  CONSTRAINT factories_max_slots_check CHECK (max_slots BETWEEN 1 AND 1000);
