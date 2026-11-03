-- +goose Up
-- The one-time conversion of every factory from connections to secrets and environment files
-- (ADR 0037 decision 19; ports.FactoryConversions, internal/moveover): one row per factory, the
-- primary key making the conversion happen once whichever goald runs it. A converted factory has
-- the version it converted (from_version) and the version Goal published (to_version); a refused
-- one has from_version and its problems, which Needs you shows until the factory publishes a
-- version on secrets. names maps each connection the version used to its secret fields' secret
-- names ({"github": {"token": "github"}}), for re-keying standing rules, knowledge sources and
-- webhooks later. No row holds a value. Rows go with the factory or the organization; the table
-- goes with the connections, a release after the conversion.
CREATE TABLE factory_conversions (
  org_id       uuid NOT NULL DEFAULT goal_org() REFERENCES organizations (id) ON DELETE CASCADE,
  factory_id   uuid NOT NULL,
  from_version uuid NOT NULL,
  to_version   uuid,
  problems     text[] NOT NULL DEFAULT '{}' CHECK (cardinality(problems) <= 100),
  names        jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(names) = 'object'),
  at           timestamptz NOT NULL,
  -- A factory is converted or refused with its problems, never both.
  CHECK ((to_version IS NULL) = (cardinality(problems) > 0)),
  PRIMARY KEY (org_id, factory_id),
  FOREIGN KEY (org_id, factory_id) REFERENCES factories (org_id, id) ON DELETE CASCADE
);

ALTER TABLE factory_conversions ENABLE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON factory_conversions
  USING (org_id = (SELECT goal_org())) WITH CHECK (org_id = (SELECT goal_org()));

GRANT SELECT, INSERT ON factory_conversions TO goal_app;
