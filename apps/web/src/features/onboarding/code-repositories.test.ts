import { describe, expect, it } from 'vitest';
import {
  ownerPattern,
  ownerUrl,
  repositoryEntries,
  repositoryLabel,
  suggestedSecret,
  withRepositories,
} from './code-repositories';

const text = `apiVersion: goal/v1
kind: Factory
spec:
  # What runs
  runtime:
    repository:
      url: https://github.com/acme/old
      token: '{{old}}'
    budget:
      perItemUsd: 5 # per item
  agents: []
`;

describe('repositoryEntries', () => {
  it('names each URL, shares the token and makes a single repository the default', () => {
    const entries = repositoryEntries(['https://github.com/acme/App.git'], 'github-acme', 'write');
    expect(entries).toEqual([
      { name: 'app', url: 'https://github.com/acme/App.git', token: '{{github-acme}}', access: 'write', default: true },
    ]);
  });

  it('keeps names unique and sets no default for several repositories or an owner pattern', () => {
    const several = repositoryEntries(['https://github.com/acme/app', 'https://gitlab.com/other/app'], 's', 'read');
    expect(several.map((e) => e.name)).toEqual(['app', 'app-2']);
    expect(several.some((e) => e.default)).toBe(false);
    const owner = repositoryEntries([ownerPattern('github.com', 'acme')], 's', 'read');
    expect(owner[0]).toMatchObject({ name: 'acme', url: 'https://github.com/acme/*', access: 'read' });
    expect(owner[0]?.default).toBeUndefined();
  });
});

describe('repositoryLabel', () => {
  it('says owner/name, or owner/* for a whole organization', () => {
    expect(repositoryLabel('https://github.com/acme/app')).toBe('acme/app');
    expect(repositoryLabel('https://github.com/acme/*')).toBe('acme/*');
  });
});

describe('withRepositories', () => {
  it('writes the entries, drops the shorthand and keeps comments and order', () => {
    const out = withRepositories(text, repositoryEntries(['https://github.com/acme/app'], 'gh', 'read'));
    expect(out).toContain('repositories:');
    expect(out).toContain('access: read');
    expect(out).toContain('token: "{{gh}}"');
    expect(out).not.toContain('repository:');
    expect(out).toContain('# What runs');
    expect(out).toContain('perItemUsd: 5 # per item');
    expect(out.indexOf('budget:')).toBeLessThan(out.indexOf('agents:'));
  });

  it('writes no entries for no code and removes an earlier list', () => {
    const out = withRepositories(text, []);
    expect(out).not.toContain('repositor');
    expect(withRepositories(out, [])).toBe(out);
  });
});

describe('ownerUrl and suggestedSecret', () => {
  it('reads an organization address with or without /*, and refuses a repository', () => {
    expect(ownerUrl('https://github.com/acme')).toBe('https://github.com/acme/*');
    expect(ownerUrl('https://github.com/acme/*')).toBe('https://github.com/acme/*');
    expect(ownerUrl('https://github.com/acme/app')).toBeUndefined();
    expect(ownerUrl('acme')).toBeUndefined();
  });

  it('names the secret after the host and the owner', () => {
    expect(suggestedSecret('https://github.com/Acme/app')).toBe('github-acme');
    expect(suggestedSecret(undefined)).toBe('code-token');
  });
});
