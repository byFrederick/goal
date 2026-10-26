// The code a CLI sign-in shows in the terminal (spec 3 §10.2 Device-Approve): eight letters and
// digits, typed by the person as ABCD-EFGH. The page only ever takes the code from what was typed,
// never from its URL, so a link someone sends cannot approve anything.

/** The characters a code has. */
export const userCodeLength = 8;

/**
 * What the field shows for what was typed or pasted ("abcd efgh", "ABCD-EFGH"): its letters and
 * digits in upper case, at most eight, with a dash after the fourth.
 */
export function formatUserCode(typed: string): string {
  const chars = typed
    .replace(/[^a-zA-Z0-9]/g, '')
    .toUpperCase()
    .slice(0, userCodeLength);
  return chars.length > 4 ? `${chars.slice(0, 4)}-${chars.slice(4)}` : chars;
}

/** Whether the field holds a whole code. */
export function isWholeCode(formatted: string): boolean {
  return formatted.replace(/-/g, '').length === userCodeLength;
}

const minute = 60_000;

/** "1 minute ago", "just now", "12 minutes ago". */
function ago(ms: number): string {
  const minutes = Math.floor(ms / minute);
  if (minutes < 1) return 'just now';
  return `${minutes} ${minutes === 1 ? 'minute' : 'minutes'} ago`;
}

/** "expires in 9 minutes", "expires in less than a minute". */
function expiresIn(ms: number): string {
  const minutes = Math.floor(ms / minute);
  if (minutes < 1) return 'expires in less than a minute';
  return `expires in ${minutes} ${minutes === 1 ? 'minute' : 'minutes'}`;
}

/**
 * When the CLI asked and until when the code works: "1 minute ago, expires in 9 minutes", or only
 * the expiry when the request's time is unknown.
 */
export function requestTimes(requestedAt: string | undefined, expiresAt: string, now: number): string {
  const left = expiresIn(Date.parse(expiresAt) - now);
  if (!requestedAt) return left.charAt(0).toUpperCase() + left.slice(1);
  return `${ago(now - Date.parse(requestedAt))}, ${left}`;
}
