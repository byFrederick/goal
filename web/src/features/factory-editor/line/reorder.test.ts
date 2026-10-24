import { describe, expect, it } from 'vitest';
import type { ModelStation } from '../../../shared/definition/model';
import { dropTarget, movedMessage, stepBlocked, stepTarget } from './reorder';

const line: ModelStation[] = [
  { id: 'inbox', kind: 'queue' },
  { id: 'spec', kind: 'agent' },
  { id: 'review', kind: 'agent' },
  { id: 'done', kind: 'done' },
];

describe('reordering stations', () => {
  it('steps up and down within the line', () => {
    expect(stepTarget(line, 'spec', -1)).toBe(0);
    expect(stepTarget(line, 'spec', 1)).toBe(2);
  });

  it('never moves Done, nor anything past it or past the first row', () => {
    expect(stepTarget(line, 'review', 1)).toBeUndefined();
    expect(stepTarget(line, 'inbox', -1)).toBeUndefined();
    expect(stepTarget(line, 'done', -1)).toBeUndefined();
    expect(stepBlocked(line, 'review', 1)).toBe('Nothing moves past Done');
    expect(stepBlocked(line, 'inbox', -1)).toBe('Already first');
    expect(stepBlocked(line, 'done', 1)).toBe('Done stays last');
    expect(stepBlocked(line, 'spec', 1)).toBeUndefined();
  });

  it('moves the last station of a line without Done to the end', () => {
    const open = line.slice(0, 3);
    expect(stepTarget(open, 'spec', 1)).toBe(2);
    expect(stepBlocked(open, 'review', 1)).toBe('Already last');
  });

  it('drops a station on another row, never on Done', () => {
    expect(dropTarget(line, 'review', 'inbox')).toBe(0);
    expect(dropTarget(line, 'inbox', 'review')).toBe(2);
    expect(dropTarget(line, 'spec', 'done')).toBeUndefined();
    expect(dropTarget(line, 'done', 'spec')).toBeUndefined();
    expect(dropTarget(line, 'spec', 'spec')).toBeUndefined();
  });

  it('says where the station went, counting from one', () => {
    expect(movedMessage('Build', 2, 7)).toBe('Build moved to position 3 of 7');
  });
});
