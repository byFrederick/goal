-- +goose Up
-- ADR 0017 decision 5: a connection's secret field whose whole value is {{name}} refers to
-- credential name instead of holding a sealed value. secret_refs maps each such field to its
-- credential's name, in the clear: a name is no secret. secret_fields still lists every secret
-- field that is set, sealed or a reference, so 00051's check is relaxed to: ciphertext is NULL
-- exactly when every set secret field is a reference (?& of no names is true, so a connection
-- without secret fields still has none). The store keeps secret_refs' keys among secret_fields.
ALTER TABLE connections
  ADD COLUMN secret_refs jsonb NOT NULL DEFAULT '{}'
    CHECK (jsonb_typeof(secret_refs) = 'object' AND octet_length(secret_refs::text) <= 8192);

ALTER TABLE connections DROP CONSTRAINT connections_check1;
ALTER TABLE connections ADD CONSTRAINT connections_check1 CHECK ((ciphertext IS NULL) = (secret_refs ?& secret_fields));

GRANT UPDATE (secret_refs) ON connections TO goal_app;
