import { describe, expect, it } from 'vitest';
import { EventMapper, toolPath } from './events.js';

describe('toolPath', () => {
  it.each([
    '/knowledge/runbooks/**/*.md',
    '/knowledge/runbooks/*.md',
    '/knowledge/runbooks/a?.md',
    '/knowledge/runbooks/[ab].md',
    '/knowledge/runbooks/{a,b}.md',
  ])('reports the folder prefix of the Glob pattern %s', (pattern) => {
    expect(toolPath('Glob', { pattern })).toBe('/knowledge/runbooks');
  });

  it('reports a Glob path over its pattern', () => {
    expect(toolPath('Glob', { path: '/knowledge/guides', pattern: '/knowledge/runbooks/*.md' })).toBe(
      '/knowledge/guides',
    );
  });

  it('reports an absolute pattern without a glob character as itself', () => {
    expect(toolPath('Glob', { pattern: '/knowledge/runbooks/a.md' })).toBe('/knowledge/runbooks/a.md');
  });

  it('reports no deeper folder than the prefix', () => {
    expect(toolPath('Glob', { pattern: '/**/*.md' })).toBe('/');
  });

  it.each([{ pattern: '**/*.md' }, { pattern: 'knowledge/runbooks/*.md' }, {}, { pattern: 3 }, { path: '' }])(
    'reports no path for the Glob input %j',
    (input) => {
      expect(toolPath('Glob', input)).toBeUndefined();
    },
  );

  it('leaves Read, Grep and other tools as before', () => {
    expect(toolPath('Read', { file_path: '/knowledge/a/b.md' })).toBe('/knowledge/a/b.md');
    expect(toolPath('Grep', { path: '/knowledge/a' })).toBe('/knowledge/a');
    expect(toolPath('Grep', { pattern: '/knowledge/runbooks/**' })).toBeUndefined();
    expect(toolPath('Bash', { pattern: '/knowledge/runbooks/**' })).toBeUndefined();
  });
});

describe('EventMapper', () => {
  it('puts the folder prefix of a pattern-only Glob in the tool_use event', () => {
    const mapper = new EventMapper(() => new Date('2026-11-03T10:00:00Z'), []);
    const events = mapper.map({
      type: 'assistant',
      parent_tool_use_id: null,
      message: {
        content: [{ type: 'tool_use', id: 't1', name: 'Glob', input: { pattern: '/knowledge/runbooks/**/*.md' } }],
      },
    } as never);
    expect(events).toEqual([expect.objectContaining({ type: 'tool_use', tool: 'Glob', path: '/knowledge/runbooks' })]);
  });
});
