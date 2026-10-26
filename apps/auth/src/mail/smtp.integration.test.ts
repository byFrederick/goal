// The SMTP adapter against Mailpit from compose.yaml (task test-integration starts it), the inbox
// the laptop setup sends to. Nothing here reaches a real mail server.
import { randomBytes } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { afterAll, describe, expect, it, vi } from 'vitest';
import { invitationMail } from './messages.js';
import { SMTPMailer } from './smtp.js';

const smtpURL = process.env.GOAL_TEST_SMTP_URL ?? 'smtp://127.0.0.1:1025';
const inboxURL = process.env.GOAL_TEST_MAILPIT_URL ?? 'http://127.0.0.1:8025';

/** Mailpit's API takes the credentials goal up writes (user:password), when there are any. */
function inboxHeaders(): Record<string, string> {
  const file = join(process.env.XDG_CONFIG_HOME ?? join(homedir(), '.config'), 'goal', 'mailpit-ui-auth');
  try {
    return { authorization: `Basic ${btoa(readFileSync(file, 'utf8').trim())}` };
  } catch {
    return {};
  }
}

const from = 'Goal <goal@localhost>';
const mailer = new SMTPMailer({ url: smtpURL, from, plaintext: true });
afterAll(() => mailer.close());

interface Summary {
  ID: string;
  From: { Address: string; Name: string };
  To: { Address: string }[];
  Subject: string;
}

async function inbox(to: string): Promise<Summary[]> {
  const res = await fetch(`${inboxURL}/api/v1/search?query=${encodeURIComponent(`to:"${to}"`)}`, {
    headers: inboxHeaders(),
  });
  expect(res.status).toBe(200);
  return ((await res.json()) as { messages: Summary[] }).messages;
}

describe('SMTPMailer', () => {
  it('delivers plain text and HTML from GOAL_SMTP_FROM', async () => {
    const to = `ada-${randomBytes(4).toString('hex')}@example.com`;

    await mailer.send(
      invitationMail(to, {
        publicURL: 'http://127.0.0.1:8080',
        invitationId: 'inv-1',
        inviterName: 'M. Okafor',
        orgName: 'Acme',
        expiresAt: new Date('2026-10-27T00:00:00Z'),
      }),
    );

    // Mailpit indexes a message shortly after it accepts it.
    const [summary, ...rest] = await vi.waitFor(async () => {
      const found = await inbox(to);
      expect(found.length).toBeGreaterThan(0);
      return found;
    });
    expect(rest).toHaveLength(0);
    expect(summary?.From).toEqual({ Address: 'goal@localhost', Name: 'Goal' });
    expect(summary?.Subject).toBe('M. Okafor invited you to Acme on Goal');

    const message = (await (
      await fetch(`${inboxURL}/api/v1/message/${summary?.ID}`, { headers: inboxHeaders() })
    ).json()) as {
      Text: string;
      HTML: string;
    };
    expect(message.Text).toContain('Join: http://127.0.0.1:8080/invite/inv-1');
    expect(message.HTML).toContain('href="http://127.0.0.1:8080/invite/inv-1"');
  });

  // Mailpit offers no STARTTLS: without plaintext (GOAL_SMTP_PLAINTEXT=on or a loopback host,
  // settings decide), nothing crosses to it in the clear.
  it('refuses a server without STARTTLS unless plaintext is allowed', async () => {
    const strict = new SMTPMailer({ url: smtpURL, from, plaintext: false });
    const to = `ada-${randomBytes(4).toString('hex')}@example.com`;
    try {
      await expect(strict.send({ to, subject: 'never sent', text: 'never sent', html: '' })).rejects.toMatchObject({
        code: 'ETLS',
      });
    } finally {
      strict.close();
    }
    expect(await inbox(to)).toHaveLength(0);
  });
});
