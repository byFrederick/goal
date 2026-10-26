import { describe, expect, it } from 'vitest';
import { invitationMail, lockoutMail, signInCodeMail } from './messages.js';

describe('signInCodeMail', () => {
  it('says the code, how long it lasts and what to do when it was not asked for', () => {
    const m = signInCodeMail('ada@example.com', '123456');

    expect(m.to).toBe('ada@example.com');
    expect(m.subject).toBe('Your Goal sign-in code: 123456');
    expect(m.text).toBe(
      "Your Goal sign-in code is 123456. It expires in 5 minutes. If you didn't ask for it, ignore this email.\n",
    );
    expect(m.html).toContain('<strong>123456</strong>');
    expect(m.html).not.toMatch(/<img|src=|https?:/);
  });
});

describe('invitationMail', () => {
  const invitation = {
    publicURL: 'https://goal.example.com',
    invitationId: '019a1c00-0000-7000-8000-0000000000aa',
    inviterName: 'M. Okafor',
    orgName: 'Acme',
    expiresAt: new Date('2026-10-27T09:00:00Z'),
  };

  it('links to the invitation on the public URL and says when it expires', () => {
    const m = invitationMail('ada@example.com', invitation);

    expect(m.subject).toBe('M. Okafor invited you to Acme on Goal');
    expect(m.text).toBe(
      'M. Okafor invited you to Acme on Goal. Join: https://goal.example.com/invite/019a1c00-0000-7000-8000-0000000000aa · expires Oct 27\n',
    );
    expect(m.html).toContain('href="https://goal.example.com/invite/019a1c00-0000-7000-8000-0000000000aa"');
    expect(m.html).not.toMatch(/<img|src=/);
  });

  it('escapes the names people chose, in the HTML and in the subject line', () => {
    const m = invitationMail('ada@example.com', {
      ...invitation,
      inviterName: '<script>alert(1)</script>',
      orgName: 'Acme & "Co"\r\nBcc: mallory@example.com',
    });

    expect(m.html).not.toContain('<script>');
    expect(m.html).toContain('&lt;script&gt;alert(1)&lt;/script&gt;');
    expect(m.html).toContain('Acme &amp; &quot;Co&quot;');
    expect(m.subject).not.toMatch(/[\r\n]/);
    expect(m.text.split('\n').filter(Boolean)).toHaveLength(1);
  });
});

describe('lockoutMail', () => {
  it('tells the person sign-in by code is locked for a day and why', () => {
    const m = lockoutMail('ada@example.com');

    expect(m.subject).toBe('Goal sign-in by code is locked');
    expect(m.text).toContain('10 wrong sign-in codes');
    expect(m.text).toContain('24 hours');
    expect(m.html).not.toMatch(/<img|src=|https?:/);
  });
});
