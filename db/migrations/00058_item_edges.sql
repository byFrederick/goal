-- +goose Up
-- Item dependencies (Store.AddEdge, RemoveEdge, Edges): item_id is not claimed while needs_id is
-- neither done nor cancelled (LockNextWaitingItem). kind says why: planned by whoever split the
-- work, linked by a person, overlap (their files overlap). Both items are of one factory and no
-- edge closes a cycle; the adapter checks both under the factory's row lock, since a CHECK sees
-- neither. Rows go with either item or the organization.
CREATE TABLE item_edges (
  org_id     uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  item_id    uuid NOT NULL,
  needs_id   uuid NOT NULL,
  kind       text NOT NULL CHECK (kind IN ('planned', 'linked', 'overlap')),
  created_by text NOT NULL CHECK (created_by ~ '^(user|service|agent|system):.+$'),  -- Actor.Ref()
  created_at timestamptz NOT NULL,
  PRIMARY KEY (item_id, needs_id),
  CHECK (item_id <> needs_id),
  FOREIGN KEY (org_id, item_id) REFERENCES work_items (org_id, id) ON DELETE CASCADE,
  FOREIGN KEY (org_id, needs_id) REFERENCES work_items (org_id, id) ON DELETE CASCADE
);
-- The primary key serves what an item needs (the claim's NOT EXISTS, the cycle walk); this index
-- what needs an item and the needs_id key's checks.
CREATE INDEX item_edges_org_needs_idx ON item_edges (org_id, needs_id);

ALTER TABLE item_edges ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON item_edges
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));
GRANT SELECT, INSERT, DELETE ON item_edges TO goal_app;
