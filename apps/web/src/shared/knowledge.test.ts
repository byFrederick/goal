import { expect, test } from 'vitest';
import { externalUrl, listOf, repositoryName, sourceSummary } from './knowledge';

test('sourceSummary says what each kind of source reads', () => {
  expect(sourceSummary({ kind: 'upload', config: {} })).toBe('Files people upload');
  expect(
    sourceSummary({
      kind: 'synced',
      config: { repository: 'https://github.com/acme/goal', branch: 'main', include: 'docs/**/*.md,\nrunbooks/*.md' },
    }),
  ).toBe('acme/goal main, docs/**/*.md, runbooks/*.md');
  expect(
    sourceSummary({
      kind: 'synced',
      config: { repository: 'https://gitlab.example.com/ops/infra.git' },
    }),
  ).toBe('ops/infra, **/*.md');
  expect(sourceSummary({ kind: 'synced', config: { spaces: 'ENG' } })).toBe('space ENG');
  expect(sourceSummary({ kind: 'synced', config: { spaces: 'ENG, SUP' } })).toBe('spaces ENG, SUP');
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
