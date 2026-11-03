-- +goose Up
-- Moving knowledge sources over (ADR 0038 decision 16): a synced source that reads with a secret
-- becomes a repository or Confluence source, its connection cleared
-- (ports.Knowledge.ConvertSource). 00093's reads check holds for the new kinds: neither reads a
-- connection, and Confluence reads with the secret the source keeps.
GRANT UPDATE (kind, connection_id) ON knowledge_sources TO goal_app;
