import { describe, expect, it } from 'vitest';
import type { Member } from '../../../api/types';
import { approvers } from './who-approves';

const member = (name: string, role: Member['role'], grants: Member['factory_roles'] = []): Member => ({
  member_id: name,
  user: { id: name, name, email: `${name}@example.com` },
  role,
  factory_roles: grants,
  joined_at: '2026-08-01T10:00:00Z',
  last_active_at: null,
  you: false,
  can_change_role: false,
  can_remove: false,
});

describe('approvers', () => {
  it('names owners, admins, and the factory’s approvers and editors, by name', () => {
    const people = [
      member('S. Lee', 'member', [{ factory_id: 'goal', role: 'approver' }]),
      member('M. Okafor', 'admin'),
      member('A. Ortiz', 'member', [{ factory_id: 'goal', role: 'viewer' }]),
      member('P. Nair', 'member', [{ factory_id: 'support', role: 'editor' }]),
      member('F. Cruz', 'owner'),
      member('J. Lindqvist', 'member', [{ factory_id: 'goal', role: 'editor' }]),
    ];
    expect(approvers(people, 'goal')).toEqual(['F. Cruz', 'J. Lindqvist', 'M. Okafor', 'S. Lee']);
  });

  it('names only owners and admins for a factory nobody has a role on yet', () => {
    expect(approvers([member('F. Cruz', 'owner'), member('S. Lee', 'member')], undefined)).toEqual(['F. Cruz']);
  });
});
