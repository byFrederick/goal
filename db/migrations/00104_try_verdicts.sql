-- +goose Up
-- Grades of tries (ADR 0039 decision 10). Code grades a try first: its output against the station's
-- contract as the proposal changes it, then the station's checks in the try's box. A try that fails
-- either is not_fixed; one that passes both waits, with no verdict, for the grader, which answers
-- fixed, not_fixed or unknown. error stays a try whose run could not be carried out (00102).
--
-- reason is why a try was graded as it was, first: the contract and its error, the failing check's
-- command, exit code and output tail, or the grader's answer. A grade needs its reason; error keeps
-- its own column. 00104, as 00103 is taken by a part of the same assembly in flight.
ALTER TABLE tries DROP CONSTRAINT tries_verdict_check;
ALTER TABLE tries ADD CONSTRAINT tries_verdict_check
  CHECK (verdict IN ('fixed', 'not_fixed', 'unknown', 'error')) NOT VALID;
ALTER TABLE tries VALIDATE CONSTRAINT tries_verdict_check;

ALTER TABLE tries ADD COLUMN reason text NOT NULL DEFAULT '' CHECK (length(reason) <= 2000);
ALTER TABLE tries ADD CONSTRAINT tries_graded_reason_check
  CHECK (verdict IS NULL OR verdict = 'error' OR length(btrim(reason)) > 0) NOT VALID;
ALTER TABLE tries VALIDATE CONSTRAINT tries_graded_reason_check;

GRANT UPDATE (reason) ON tries TO goal_app;
