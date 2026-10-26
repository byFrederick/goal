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

describe('holds on an archived factory', () => {
  const archived = {
    archived_at: '2026-10-25T09:00:00Z',
    can: [
      'factory:manage',
      'item:create',
      'item:operate',
      'item:decide',
      'andon:manage',
      'definition:edit',
      'definition:publish',
      'factory:archive',
    ],
  };

  it.each([
    'factory:manage',
    'item:create',
    'item:operate',
    'item:decide',
    'andon:manage',
    'definition:edit',
    'definition:publish',
  ] as const)('holds no factory write, %s, while it is archived', (permission) => {
    expect(holds(archived, permission)).toBe(false);
  });

  it('still holds an organization permission, which restores it', () => {
    expect(holds(archived, 'factory:archive')).toBe(true);
  });

  it('holds its write permissions again once restored', () => {
    const { archived_at: _archived, ...restored } = archived;
    expect(holds(restored, 'item:create')).toBe(true);
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
