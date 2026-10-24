import { describe, expect, it } from 'vitest';
import { factoryRoleNote, holds } from './permissions';

describe('holds', () => {
  it('is true only for a permission goald lists', () => {
    const factory = { can: ['factory:read', 'item:create', 'item:decide'] };
    expect(holds(factory, 'item:decide')).toBe(true);
    expect(holds(factory, 'definition:edit')).toBe(false);
  });

  it('holds nothing while goald has not said', () => {
    expect(holds(undefined, 'item:decide')).toBe(false);
    expect(holds({}, 'org:manage')).toBe(false);
    expect(holds({ can: [] }, 'org:manage')).toBe(false);
  });
});

describe('factoryRoleNote', () => {
  it('names the roles that hold less than an editor', () => {
    expect(factoryRoleNote('viewer', 'sre')).toBe('You’re a viewer of sre');
    expect(factoryRoleNote('approver', 'sre')).toBe('You’re an approver of sre');
  });

  it('says nothing for editors and when goald names no role', () => {
    expect(factoryRoleNote('editor', 'sre')).toBeNull();
    expect(factoryRoleNote(null, 'sre')).toBeNull();
    expect(factoryRoleNote(undefined, 'sre')).toBeNull();
  });
});
