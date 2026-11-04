-- +goose Up
-- Judges, eval runs and bake-offs are removed (ADR 0039 decision 21): Jonah's evidence, tries of
-- past items and kept tests replace them. No organization has a judge score, an eval run or a
-- bake-off, so nothing is converted. The children go first: eval_run_arms (00077) and
-- eval_results (00068) reference eval_runs. Their policies, indexes and grants go with them. No
-- other table references these, and goal_purge_org (00042) and goal_delete_factory (00061) never
-- named them (they cascaded), so neither needs a change. Proposals and their tables are kept.
DROP TABLE eval_run_arms;
DROP TABLE eval_results;
DROP TABLE eval_runs;
DROP TABLE judge_scores;
