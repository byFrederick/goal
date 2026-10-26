import { expect, test } from 'vitest';
import { externalUrl, listOf, repositoryName, sourceSummary } from './knowledge';

test('sourceSummary says what each kind of source reads', () => {
  expect(sourceSummary({ kind: 'upload', config: {} })).toBe('Files people upload');
  expect(
    sourceSummary({
      kind: 'synced',
      connection: 'github',
      config: { repository: 'https://github.com/acme/goal', branch: 'main', include: 'docs/**/*.md,\nrunbooks/*.md' },
    }),
  ).toBe('github, acme/goal main, docs/**/*.md, runbooks/*.md');
  expect(
    sourceSummary({
      kind: 'synced',
      connection: 'gitlab-infra',
      config: { repository: 'https://gitlab.example.com/ops/infra.git' },
    }),
  ).toBe('gitlab-infra, ops/infra, **/*.md');
  expect(sourceSummary({ kind: 'synced', connection: 'confluence', config: { spaces: 'ENG' } })).toBe(
    'confluence, space ENG',
  );
  expect(sourceSummary({ kind: 'synced', connection: 'confluence', config: { spaces: 'ENG, SUP' } })).toBe(
    'confluence, spaces ENG, SUP',
  );
  expect(sourceSummary({ kind: 'live', connection: 'slack', config: {} })).toBe('slack, searched by its own tools');
});

test('lists are separated by commas or new lines, as goald reads them', () => {
  expect(listOf(' a, b\n\nc ,')).toEqual(['a', 'b', 'c']);
  expect(listOf(undefined)).toEqual([]);
  expect(repositoryName('not a url')).toBe('not a url');
});

test('externalUrl links a synced document only at an http or https address', () => {
  expect(externalUrl('https://github.com/acme/runbooks/blob/main/oom.md')).toBe(
    'https://github.com/acme/runbooks/blob/main/oom.md',
  );
  expect(externalUrl('http://wiki.example.com/x')).toBe('http://wiki.example.com/x');
  for (const bad of [
    'javascript:alert(1)',
    'data:text/html,<b>x</b>',
    'JAVASCRIPT:x',
    'file:///etc/passwd',
    '',
    null,
  ]) {
    expect(externalUrl(bad)).toBeUndefined();
  }
  expect(externalUrl(undefined)).toBeUndefined();
});
