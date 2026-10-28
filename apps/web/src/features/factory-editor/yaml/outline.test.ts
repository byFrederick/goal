import { describe, expect, it } from 'vitest';
import { outlineOf } from './outline';

const yaml = [
  '# a comment',
  'apiVersion: goal/v1',
  'metadata:',
  '  name: software',
  'spec:',
  '  runtime:',
  '    model: claude-opus-5-5',
  '    size: xlarge',
  '  connections: [github, cloudflare, context7]',
  '  rules:',
  '    - { name: secrets, source: ./rules/secrets.md }',
  '    - { name: evidence, source: ./rules/evidence.md }',
  '  board:',
  '    stations:',
  '      - { id: spec }',
].join('\n');

describe('outlineOf', () => {
  it('lists the keys with their depth and the line each starts on', () => {
    const items = outlineOf(yaml);
    expect(items.map((i) => [i.key, i.line, i.depth])).toEqual([
      ['apiVersion', 2, 0],
      ['metadata', 3, 0],
      ['name', 4, 1],
      ['spec', 5, 0],
      ['runtime', 6, 1],
      ['model', 7, 2],
      ['size', 8, 2],
      ['connections', 9, 1],
      ['rules', 10, 1],
      ['board', 13, 1],
      ['stations', 14, 2],
    ]);
  });

  it('counts the items of a flow or a block list', () => {
    const items = outlineOf(yaml);
    expect(items.find((i) => i.key === 'connections')?.count).toBe(3);
    expect(items.find((i) => i.key === 'rules')?.count).toBe(2);
    expect(items.find((i) => i.key === 'stations')?.count).toBe(1);
    expect(items.find((i) => i.key === 'runtime')?.count).toBeUndefined();
  });

  it('counts the problems on the lines of each key, and drops the count for them', () => {
    const items = outlineOf(yaml, [8]);
    const runtime = items.find((i) => i.key === 'runtime');
    expect(runtime?.problems).toBe(1);
    expect(runtime?.count).toBeUndefined();
    expect(items.find((i) => i.key === 'connections')?.problems).toBe(0);
  });

  it('is empty for text with no keys', () => {
    expect(outlineOf('# only a comment\n- a\n- b\n')).toEqual([]);
  });
});
