// The messages the auth service sends (spec 3 §8.7): plain text plus minimal HTML, no remote
// images or tracking, every name a person chose escaped, and links only to the public URL.
import type { Mail } from './mailer.js';

export function signInCodeMail(to: string, code: string): Mail {
  const text = `Your Goal sign-in code is ${code}. It expires in 5 minutes. If you didn't ask for it, ignore this email.`;
  return {
    to,
    subject: `Your Goal sign-in code: ${code}`,
    text: `${text}\n`,
    html: page(
      `<p>Your Goal sign-in code is <strong>${escapeHTML(code)}</strong>.</p>` +
        `<p>It expires in 5 minutes. If you didn't ask for it, ignore this email.</p>`,
    ),
  };
}

export interface Invitation {
  /** GOAL_PUBLIC_URL: the only origin a link may name, never the request's Host. */
  publicURL: string;
  invitationId: string;
  inviterName: string;
  orgName: string;
  expiresAt: Date;
}

export function invitationMail(to: string, inv: Invitation): Mail {
  const inviter = oneLine(inv.inviterName) || 'Someone';
  const org = oneLine(inv.orgName);
  const url = `${inv.publicURL}/invite/${encodeURIComponent(inv.invitationId)}`;
  const expires = inv.expiresAt.toLocaleDateString('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' });
  return {
    to,
    subject: `${inviter} invited you to ${org} on Goal`,
    text: `${inviter} invited you to ${org} on Goal. Join: ${url} · expires ${expires}\n`,
    html: page(
      `<p>${escapeHTML(inviter)} invited you to ${escapeHTML(org)} on Goal.</p>` +
        `<p><a href="${escapeHTML(url)}">Join ${escapeHTML(org)}</a> · expires ${escapeHTML(expires)}</p>`,
    ),
  };
}

export function lockoutMail(to: string): Mail {
  const text =
    'Sign-in by email code to Goal is locked for this address after 10 wrong sign-in codes in a day. ' +
    'It unlocks within 24 hours. If you did not try to sign in, someone else may be guessing codes: no code was ' +
    'sent to them, and your account is safe.';
  return {
    to,
    subject: 'Goal sign-in by code is locked',
    text: `${text}\n`,
    html: page(`<p>${escapeHTML(text)}</p>`),
  };
}

function page(body: string): string {
  return `<!doctype html><html><body>${body}</body></html>`;
}

/** oneLine collapses whitespace, line breaks included, so a name cannot add a header line. */
function oneLine(s: string): string {
  return s.replace(/\s+/g, ' ').trim();
}

const entities: Record<string, string> = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };

function escapeHTML(s: string): string {
  return s.replace(/[&<>"']/g, (c) => entities[c] ?? c);
}
