// Why goald refused to set an item's lane (ADR 0016): each refused item's reason comes from its
// problem type, so the words stay plain whatever goald's detail says.
import { ApiError } from '@goal/api-client';

const reasons: Record<string, string> = {
  'item-in-progress': 'It is in progress. Pause it or move it first.',
  'item-closed': 'It is done or cancelled.',
  'item-is-assembly': 'It is an assembly. Its parts carry their own lanes.',
  'same-lane': 'It is already in that lane.',
  'unknown-lane': 'The line has no station that routes that lane.',
  'item-changed': 'It changed while you were choosing. Refresh and try again.',
};

/** The reason one refused item gives, in plain words. */
export function laneRefusalOf(err: unknown): string {
  if (err instanceof ApiError && err.code && reasons[err.code]) return reasons[err.code] as string;
  return 'goald did not set the lane. Try again once it is answering.';
}
