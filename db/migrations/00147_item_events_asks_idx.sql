-- +goose Up
-- The events an item's open question and tool approval are read from (domain.AskKinds: questions
-- asked and answered, tool approvals requested and decided, and the decisions that withdraw
-- them), by item in append order, for AskEvents. item_events_item_idx reads every event of the
-- items asked for and drops the others by kind; an item that waits on a person after a long run
-- has thousands of them, so at production size the planner scans the table instead. A partial
-- index holds only these few kinds. AskEvents names the same kinds, so the planner can use it.
CREATE INDEX item_events_asks_idx ON item_events (item_id, id)
  WHERE kind IN ('question_asked', 'question_answered', 'tool_approval_requested', 'tool_approval_granted',
                 'tool_approval_denied', 'moved', 'paused', 'rejected', 'cancelled');
