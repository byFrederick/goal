import { describe, expect, test } from 'vitest';
import { configErrors, trimmed } from './config-fields';
import { fromDetail } from './facts';
import { sourceOf } from './test-harness';

describe('Confluence root page', () => {
  const base = { site: 'https://acme.atlassian.net', email: 'bot@example.com', spaces: 'ENG' };

  test('is optional, and a page id is digits only', () => {
    expect(configErrors('confluence', base)).toEqual({});
    expect(configErrors('confluence', { ...base, root_page: ' 12345 ' })).toEqual({});
    expect(configErrors('confluence', { ...base, root_page: 'abc' }).root_page).toBe(
      'A page id is digits only, such as 12345.',
    );
  });

  test('is left out of the request when empty', () => {
    expect(trimmed({ ...base, root_page: '  ' })).toEqual(base);
    expect(trimmed({ ...base, root_page: ' 12345 ' }).root_page).toBe('12345');
  });

  test('the source’s one-line summary names it when set', () => {
    expect(fromDetail(sourceOf({ name: 'x', kind: 'confluence', config: base }))).toBe('Space ENG');
    expect(fromDetail(sourceOf({ name: 'x', kind: 'confluence', config: { ...base, root_page: '12345' } }))).toBe(
      'Space ENG, page 12345',
    );
  });
});
