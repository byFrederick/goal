import { describe, expect, it } from 'vitest';
import type { Member } from '../../api/types';
import {
  byRole,
  countTitle,
  factorySummary,
  invitationLine,
  lastActive,
  matches,
  parseEmails,
  roleChange,
} from './people';

const factories = [
  { id: 'f-goal', name: 'goal' },
  { id: 'f-sre', name: 'sre' },
  { id: 'f-support', name: 'support' },
];

const member = (over: Partial<Member>): Member => ({
  member_id: 'm1',
  user: { id: 'u1', name: 'J. Lindqvist', email: 'j.lindqvist@example.com' },
  role: 'member',
  factory_roles: [],
  joined_at: '2026-08-01T10:00:00Z',
  last_active_at: null,
  you: false,
  can_change_role: false,
  can_remove: false,
  ...over,
});

describe('factorySummary', () => {
  it('says owners and admins are editors everywhere, whatever rows they hold', () => {
    expect(
      factorySummary(member({ role: 'admin', factory_roles: [{ factory_id: 'f-sre', role: 'viewer' }] }), factories),
    ).toBe('Editor in every factory');
  });

  it('names a member’s factories by name, with their role, alphabetically', () => {
    const m = member({
      factory_roles: [
        { factory_id: 'f-support', role: 'viewer' },
        { factory_id: 'f-sre', role: 'approver' },
      ],
    });
    expect(factorySummary(m, factories)).toBe('sre approver, support viewer');
  });

  it('leaves out factories the caller does not see and says when nothing is left', () => {
    expect(factorySummary(member({ factory_roles: [{ factory_id: 'f-hidden', role: 'editor' }] }), factories)).toBe(
      'No factories',
    );
  });
});

describe('lastActive', () => {
  const now = new Date('2026-10-24T15:00:00');

  it.each([
    ['2026-10-24T08:00:00', 'Today'],
    ['2026-10-23T23:59:00', 'Yesterday'],
    ['2026-10-20T12:00:00', 'Oct 20'],
    [null, 'Never'],
  ])('%s reads %s', (at, want) => {
    expect(lastActive(at, now)).toBe(want);
  });
});

describe('roleChange', () => {
  const owner = member({ role: 'owner', user: { id: 'u0', name: 'F. Cruz', email: 'f.cruz@example.com' } });

  it('is read-only, without a reason, for someone who cannot manage people', () => {
    expect(roleChange(member({}), { manages: false, role: 'member' }, 1, 'Acme')).toEqual({ editable: false });
  });

  it('follows the server when it allows the change', () => {
    expect(roleChange(member({ can_change_role: true }), { manages: true, role: 'admin' }, 1, 'Acme')).toEqual({
      editable: true,
    });
  });

  it('says only owners change an owner, to an admin', () => {
    expect(roleChange(owner, { manages: true, role: 'admin' }, 2, 'Acme')).toEqual({
      editable: false,
      reason: 'Only owners change an owner',
    });
  });

  it('says the organization keeps an owner, to its only owner', () => {
    expect(roleChange({ ...owner, you: true }, { manages: true, role: 'owner' }, 1, 'Acme')).toEqual({
      editable: false,
      reason: 'Acme needs at least one owner',
    });
  });
});

describe('parseEmails', () => {
  it('takes one address per line (or comma), trimmed, lower-cased, once each', () => {
    expect(parseEmails(' D.Ross@example.com\nk.ito@example.com, d.ross@example.com\n\n')).toEqual({
      emails: ['d.ross@example.com', 'k.ito@example.com'],
      invalid: [],
    });
  });

  it('lists what is not an address', () => {
    expect(parseEmails('d.ross@example.com\nnobody\n@example.com').invalid).toEqual(['nobody', '@example.com']);
  });
});

describe('the rest of the page', () => {
  it('counts members and invitations', () => {
    expect(countTitle(6, 1)).toBe('6 members, 1 invite waiting');
    expect(countTitle(1, 0)).toBe('1 member');
    expect(countTitle(2, 3)).toBe('2 members, 3 invites waiting');
  });

  it('describes an invitation by when, who and until when', () => {
    expect(
      invitationLine({
        created_at: '2026-10-24T12:00:00Z',
        expires_at: '2026-10-27T12:00:00Z',
        inviter: { id: 'u', name: 'M. Okafor' },
      }),
    ).toBe('Invited Oct 24 by M. Okafor, expires Oct 27');
  });

  it('lists owners first, then admins, then members, each by name', () => {
    const people = [
      member({ role: 'member', user: { id: 'a', name: 'A. Ortiz', email: 'a' } }),
      member({ role: 'admin', user: { id: 'm', name: 'M. Okafor', email: 'm' } }),
      member({ role: 'owner', user: { id: 'f', name: 'F. Cruz', email: 'f' } }),
      member({ role: 'member', user: { id: 'j', name: 'J. Lindqvist', email: 'j' } }),
    ];
    expect(byRole(people).map((m) => m.user.name)).toEqual(['F. Cruz', 'M. Okafor', 'A. Ortiz', 'J. Lindqvist']);
  });

  it('finds a person by name or email, case aside', () => {
    expect(matches(member({}), 'lind')).toBe(true);
    expect(matches(member({}), 'J.LINDQVIST@')).toBe(true);
    expect(matches(member({}), 'okafor')).toBe(false);
    expect(matches(member({}), '  ')).toBe(true);
  });
});
