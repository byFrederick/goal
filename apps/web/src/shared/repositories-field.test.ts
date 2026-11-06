import { describe, expect, it } from 'vitest';
import { choicesOf } from './repositories-field';

const entry = (url: string, extra: Record<string, unknown> = {}) => ({
  name: 'x',
  url,
  access: 'read' as const,
  ...extra,
});

describe('choicesOf', () => {
  it('lists single repositories as owner/name and starts with the default entry', () => {
    const c = choicesOf([
      entry('https://github.com/acme/api.git', { default: true }),
      entry('https://gitlab.example.com/group/sub/web'),
    ]);
    expect(c).toEqual({ options: ['acme/api', 'group/sub/web'], owners: [], initial: 'acme/api' });
  });

  it('keeps owner-wide entries as owners, never as an option or a default', () => {
    const c = choicesOf([entry('https://github.com/acme/*', { default: true })]);
    expect(c).toEqual({ options: [], owners: ['acme'], initial: undefined });
  });

  it('has nothing for no entries', () => {
    expect(choicesOf([])).toEqual({ options: [], owners: [], initial: undefined });
  });
});
