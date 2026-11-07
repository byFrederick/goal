/**
 * The box's git credential helper for a station run's repositories (ADR 0041 decision 5). git runs
 * it with credential.useHttpPath on, so each request names a repository's path:
 *
 *   node credential.js <logins file> <record file> get|store|erase
 *
 * For get it answers with the login of the longest prefix that covers the repository (one
 * repository, or an owner's every repository when the prefix ends in /), compared in lower case
 * with .git stripped, and appends the repository's URL to the record file. A repository no login
 * covers, another host or another action gets no answer, so git asks its next helper or fails.
 * Prepare writes the logins file (0600, under the box's home); nothing here reads the environment.
 * It imports only node:, so node runs it as it is.
 */
import { appendFileSync, readFileSync, realpathSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

/** One login as prepare writes it: a URL prefix, the user name and the token. */
export interface Login {
  prefix: string;
  username: string;
  password: string;
}

/** A repository's URL as the helper compares and records it: lower case, without a trailing / or .git. */
export function repositoryUrl(url: string): string {
  let u = url.trim().toLowerCase();
  while (u.endsWith('/')) u = u.slice(0, -1);
  if (u.endsWith('.git')) u = u.slice(0, -4);
  return u;
}

/** A login's prefix as the helper compares it: an owner's keeps its trailing /, a repository's is a repositoryUrl. */
function prefixOf(prefix: string): string {
  return prefix.endsWith('/') ? prefix.trim().toLowerCase() : repositoryUrl(prefix);
}

/** The login of the longest prefix that covers url, a repositoryUrl; undefined when none does. */
export function loginFor(logins: Login[], url: string): Login | undefined {
  let best: { login: Login; length: number } | undefined;
  for (const login of logins) {
    const p = prefixOf(login.prefix);
    const covers = p.endsWith('/') ? url.startsWith(p) && url.length > p.length : url === p;
    if (covers && (!best || p.length > best.length)) best = { login, length: p.length };
  }
  return best?.login;
}

/** The repository git's request names (protocol, host, path), as a repositoryUrl; undefined without a path. */
export function requestedUrl(input: string): string | undefined {
  const attrs = new Map<string, string>();
  for (const line of input.split('\n')) {
    if (line === '') break;
    const eq = line.indexOf('=');
    if (eq > 0) attrs.set(line.slice(0, eq), line.slice(eq + 1));
  }
  const host = attrs.get('host');
  const path = attrs.get('path');
  if (attrs.get('protocol') !== 'https' || !host || !path) return undefined;
  return repositoryUrl(`https://${host}/${path}`);
}

/**
 * The helper's answer to one request: git's credential lines for get with a covering login, and
 * '' otherwise. answered is called with the repository's URL when it answers.
 */
export function answer(action: string, input: string, logins: Login[], answered: (url: string) => void): string {
  if (action !== 'get') return '';
  const url = requestedUrl(input);
  if (!url) return '';
  const login = loginFor(logins, url);
  if (!login) return '';
  answered(url);
  return `username=${login.username}\npassword=${login.password}\n`;
}

function main(argv: string[]): void {
  const [loginsFile, recordFile, action] = argv;
  if (!loginsFile || !recordFile || !action) return;
  try {
    const logins = JSON.parse(readFileSync(loginsFile, 'utf8')) as Login[];
    const out = answer(action, readFileSync(0, 'utf8'), logins, (url) =>
      appendFileSync(recordFile, `${url}\n`, { mode: 0o600 }),
    );
    process.stdout.write(out);
  } catch {
    // No answer: git asks the next helper or fails without a prompt. A JSON error would quote the
    // file, which holds the tokens, so the message is fixed.
    process.stderr.write('goal-agent credential helper: cannot read the logins or write the record\n');
  }
}

const self = process.argv[1];
if (self && import.meta.url === pathToFileURL(realpathSync(self)).href) main(process.argv.slice(2));
