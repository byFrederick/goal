import { describe, expect, it } from 'vitest';
import { addEntry, editEntry, memoryProblem, readMemory, removeEntry, writeMemory } from './memory-file';

const now = new Date('2026-11-03T10:00:00Z');

describe('memory.yaml', () => {
  it('reads the entries of a file and none of an empty or broken one', () => {
    const text =
      '- text: Use pnpm.\n  from: [acme#12]\n  added: "2026-10-01"\n- text: Tests run with task.\n  added: "2026-10-02"\n';
    expect(readMemory(text)).toEqual([
      { text: 'Use pnpm.', from: ['acme#12'], added: '2026-10-01' },
      { text: 'Tests run with task.', from: [], added: '2026-10-02' },
    ]);
    expect(readMemory(undefined)).toEqual([]);
    expect(readMemory('a: [')).toEqual([]);
    expect(readMemory('key: value')).toEqual([]);
  });

  it('adds, edits and removes, and writes a file it reads back', () => {
    let entries = addEntry([], '  Use pnpm. ', now);
    entries = addEntry(entries, 'Tests run with task.', now);
    entries = editEntry(entries, 0, 'Use pnpm, not npm.');
    entries = removeEntry(entries, 1);
    expect(entries).toEqual([{ text: 'Use pnpm, not npm.', from: [], added: '2026-11-03' }]);
    expect(readMemory(writeMemory(entries))).toEqual(entries);
    expect(writeMemory([])).toBe('');
  });

  it('refuses an empty fact, one over 300 characters and a 51st', () => {
    expect(memoryProblem([], -1, ' ')).toBe('Write the fact.');
    expect(memoryProblem([], -1, 'x'.repeat(301))).toBe('At most 300 characters (now 301).');
    const many = Array.from({ length: 50 }, (_, i) => ({ text: `fact ${i}`, from: [], added: '2026-11-03' }));
    expect(memoryProblem(many, -1, 'one more')).toBe('Factory memory holds at most 50 facts.');
    expect(memoryProblem(many, 3, 'a changed fact')).toBeUndefined();
  });

  it('refuses a fact that takes the file past 8 KB', () => {
    const big = Array.from({ length: 28 }, (_, i) => ({
      text: `${i}${'x'.repeat(290)}`,
      from: [],
      added: '2026-11-03',
    }));
    expect(memoryProblem(big, -1, 'x'.repeat(100))).toBe('Factory memory would pass 8 KB.');
  });
});
