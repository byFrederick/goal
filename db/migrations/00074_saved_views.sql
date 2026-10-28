-- +goose Up
-- Saved views of the Items page (ADR 0020, decision 1; ports.Store ListSavedViews,
-- CreateSavedView, UpdateSavedView, DeleteSavedView): a name plus the page's URL search params,
-- filters a JSON object of string lists. owner_id is the Better Auth user who made the view, with
-- no foreign key: a member leaving keeps the shared views they made. A personal view's name is
-- unique for its owner and a shared view's within its factory, case ignored; the adapter maps
-- either index's violation to domain.ErrSavedViewNameTaken. Rows go with the factory or the
-- organization.
CREATE TABLE saved_views (
  id         uuid PRIMARY KEY,
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id uuid NOT NULL,
  owner_id   uuid NOT NULL,
  name       text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
  shared     boolean NOT NULL,
  filters    jsonb NOT NULL CHECK (jsonb_typeof(filters) = 'object'),
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE
);
-- The list, the purge by organization and the cascade from factories read this index; the two
-- partial ones only hold the name rules.
CREATE INDEX saved_views_org_factory_idx ON saved_views (org_id, factory_id);
CREATE UNIQUE INDEX saved_views_personal_name_key ON saved_views (factory_id, owner_id, lower(name)) WHERE NOT shared;
CREATE UNIQUE INDEX saved_views_shared_name_key ON saved_views (factory_id, lower(name)) WHERE shared;

ALTER TABLE saved_views ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON saved_views
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
GRANT SELECT, INSERT, UPDATE, DELETE ON saved_views TO goal_app;
