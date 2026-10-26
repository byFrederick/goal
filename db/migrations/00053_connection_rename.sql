-- +goose Up
-- Connections can be renamed (spec 4 §8.2, PATCH …/connections/{connection} {name}): goal_app may
-- update a connection's name too. Its sealed values stay valid, being bound to its id
-- (domain.ConnectionAAD), and connections_org_id_name_key keeps the name unique in its organization.
GRANT UPDATE (name) ON connections TO goal_app;
