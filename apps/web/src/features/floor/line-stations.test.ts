import { describe, expect, it } from 'vitest';
import type { BoardStation } from '../../api/types';
import { waitsOf } from './line-stations';

const done: BoardStation = {
  id: 'done',
  kind: 'done',
  agents: [],
  wip_limit: null,
  waiting: 0,
  in_progress: 0,
  items: [],
  busy_ratio: null,
  send_back_ratio: null,
  queue_trend: null,
  oldest_waiting_seconds: null,
  done_today: 3,
  lanes: [],
};

describe('waitsOf', () => {
  it('for a done station on a line without code says done today', () => {
    expect(waitsOf(done, null, 5)).toEqual(['3 done today', '5 this week']);
  });

  it('keeps merged today on a line with code', () => {
    expect(waitsOf(done, null, 5, true)).toEqual(['3 merged today', '5 this week']);
  });
});
