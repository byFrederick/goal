-- +goose Up
-- Draft requests are not built yet: no goald runs them, so the create route refuses each with 501
-- not-built (ADR 0021). A request queued before that refusal would never run, and as an open
-- request it holds draft_requests_one_open_idx, which blocks every later ask on its factory. This
-- ends every queued or running request failed, with the reason in its error; done and failed
-- requests keep their result. It runs once as goal_owner, the tables' owner, which row-level
-- security does not filter (00006), so it reaches every organization's rows.
UPDATE draft_requests
SET status = 'failed',
    finished_at = now(),
    lease_holder = NULL,
    lease_expires_at = NULL,
    error = 'draft requests are not built yet: goald has no runner for them, so this request never ran; ask again once they are built'
WHERE status IN ('queued', 'running');
