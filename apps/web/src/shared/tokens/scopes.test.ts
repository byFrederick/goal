import { describe, expect, it } from 'vitest';
import {
  emptyKeyForm,
  expiryText,
  keyFormErrors,
  lastUsedText,
  needsText,
  newKeyBody,
  presetOf,
  presets,
  scopeGroups,
  withinCeiling,
} from './scopes';

const scope = (id: string) => {
  const s = scopeGroups.flatMap((g) => g.scopes).find((x) => x.id === id);
  if (!s) throw new Error(id);
  return s;
};

describe('the ceiling', () => {
  it('says which scopes it cannot use, and why', () => {
    expect(needsText('approver', scope('definition:publish'))).toBe('Needs Editor');
    expect(needsText('viewer', scope('item:create'))).toBe('Needs Approver');
    expect(needsText('viewer', scope('factory:create'))).toBeUndefined();
    expect(needsText('editor', scope('definition:publish'))).toBeUndefined();
  });

  it('drops the scopes above it when it is lowered', () => {
    const chosen = ['org:read', 'factory:read', 'item:create', 'definition:edit', 'factory:create'];
    expect(withinCeiling(chosen, 'viewer')).toEqual(['org:read', 'factory:read', 'factory:create']);
    expect(withinCeiling(chosen, 'editor')).toEqual(chosen);
  });

  it('never sends a scope above it, even one chosen before lowering it', () => {
    const body = newKeyBody({ ...emptyKeyForm, name: ' ci ', ceiling: 'viewer', scopes: ['org:read', 'item:create'] });
    expect(body).toEqual({ name: 'ci', scopes: ['org:read'], role_ceiling: 'viewer', expires_in_days: 90 });
  });
});

describe('presets', () => {
  it('are recognized only when the choice is exactly theirs', () => {
    for (const p of presets) expect(presetOf(p.ceiling, [...p.scopes].reverse())).toBe(p.id);
    expect(presetOf('approver', ['org:read', 'factory:read', 'item:create', 'item:decide'])).toBeUndefined();
    expect(presetOf('editor', ['org:read', 'factory:read', 'item:create'])).toBeUndefined();
  });

  it('fit their own ceiling', () => {
    for (const p of presets) expect(withinCeiling(p.scopes, p.ceiling)).toEqual(p.scopes);
  });
});

describe('the form', () => {
  it('needs a name, a factory when restricted, and a scope', () => {
    expect(keyFormErrors({ ...emptyKeyForm, factories: [] })).toEqual({
      name: 'Give the key a name',
      factories: 'Choose at least one factory',
      scopes: 'Choose at least one thing it can do',
    });
    expect(keyFormErrors({ ...emptyKeyForm, name: 'ci', scopes: ['org:read'] })).toEqual({});
  });

  it('names its factories only when restricted', () => {
    const base = { ...emptyKeyForm, name: 'ci', scopes: ['factory:read'] };
    expect(newKeyBody({ ...base, factories: ['f1'] }).factories).toEqual(['f1']);
    expect('factories' in newKeyBody(base)).toBe(false);
  });
});

describe('a row', () => {
  const now = Date.parse('2026-10-24T09:00:00Z');

  it('flags an expiry under 14 days', () => {
    expect(expiryText('2026-11-02T09:00:00Z', now).soon).toBe('in 9 days');
    expect(expiryText('2026-12-31T09:00:00Z', now)).toEqual({ day: 'Dec 31', soon: undefined });
    expect(expiryText('2026-10-20T09:00:00Z', now).soon).toBe('expired');
  });

  it('says when the key was last used', () => {
    expect(lastUsedText(null, now)).toBe('Never');
    expect(lastUsedText('2026-10-24T07:00:00Z', now)).toBe('2 h ago');
    expect(lastUsedText('2026-10-24T08:59:30Z', now)).toBe('Just now');
  });
});
