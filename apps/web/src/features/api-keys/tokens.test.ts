import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';
import {
  cliWorkPreset,
  emptyTokenForm,
  highImpactScopes,
  holdsScope,
  newTokenBody,
  tokenFormErrors,
  tokenScopeGroups,
  tokenScopes,
} from './tokens';

const scope = (id: string) => {
  const s = tokenScopeGroups.flatMap((g) => g.scopes).find((x) => x.id === id);
  if (!s) throw new Error(id);
  return s;
};

describe('what a personal token can do', () => {
  it('offers exactly what contracts/api/permissions.yaml lets tokens carry (but authenticated, which every token has)', () => {
    const table = parse(
      readFileSync(join(import.meta.dirname, '../../../../../contracts/api/permissions.yaml'), 'utf8'),
    ) as {
      permissions: { name: string; pat?: boolean }[];
    };
    const pat = table.permissions.filter((p) => p.pat && p.name !== 'authenticated').map((p) => p.name);
    expect([...tokenScopes].sort()).toEqual(pat.sort());
  });

  it('never puts a high-impact scope in the CLI: work preset', () => {
    expect(highImpactScopes).toEqual([
      'definition:edit',
      'definition:publish',
      'factory:create',
      'factory:archive',
      'factory:delete',
    ]);
    for (const s of highImpactScopes) expect(cliWorkPreset).not.toContain(s);
    expect([...cliWorkPreset].sort()).toEqual(
      [
        'andon:manage',
        'connection:read',
        'factory:read',
        'item:create',
        'item:decide',
        'item:operate',
        'knowledge:read',
        'org:read',
      ].sort(),
    );
  });

  it('is offered only when the person holds it: an org scope by the organization, a factory scope on some factory', () => {
    expect(holdsScope(scope('org:read'), ['org:read'], [])).toBe(true);
    expect(holdsScope(scope('factory:create'), ['org:read'], [])).toBe(false);
    expect(holdsScope(scope('item:decide'), ['org:read'], [{ can: ['factory:read'] }, { can: ['item:decide'] }])).toBe(
      true,
    );
    expect(holdsScope(scope('definition:publish'), ['org:read'], [{ can: ['factory:read', 'item:decide'] }])).toBe(
      false,
    );
    expect(holdsScope(scope('org:read'), undefined, [])).toBe(false);
  });
});

describe('the New token form', () => {
  it('starts empty, 30 days, every time', () => {
    expect(emptyTokenForm).toEqual({ name: '', days: 30, scopes: [] });
    expect(tokenFormErrors(emptyTokenForm)).toEqual({
      name: 'Give the token a name',
      scopes: 'Choose at least one thing it can do',
    });
  });

  it('sends only scopes tokens may carry, the name trimmed', () => {
    expect(
      newTokenBody({
        name: '  laptop ',
        days: 7,
        scopes: ['item:create', 'member:manage', 'org:read', 'api_key:manage'],
      }),
    ).toEqual({ name: 'laptop', scopes: ['org:read', 'item:create'], expires_in_days: 7 });
    expect(tokenFormErrors({ name: 'x'.repeat(81), days: 30, scopes: ['org:read'] })).toEqual({
      name: 'At most 80 characters',
    });
  });

  // Spec 3 §11.1 row 49: a link must not set a token up for someone else's use. The dialog keeps
  // its own state only; it never reads the URL (search params, hash or location).
  it('takes nothing from the URL', () => {
    const source = readFileSync(join(import.meta.dirname, 'new-token-dialog.tsx'), 'utf8');
    expect(source).not.toMatch(/useSearch|useLocation|useParams|useRouterState|window\.location|URLSearchParams/);
  });
});
