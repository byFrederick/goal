// The Code step's URL state (frontend.md: a selection lives in the URL, so a linked step shows the same
// choice): the repository's https URL and the name of the secret that holds its token. The token itself
// is never in the URL. Malformed values are dropped, never thrown on. Steps 3 and 4 read the same search,
// and from step 3 on the factory created there (`factory`, its id) rides along.
import { secretNamePattern } from '../secrets/words';

export interface CodeSearch {
  /** The repository's https URL, such as https://github.com/acme/app. */
  url?: string | undefined;
  /** The secret that holds the token, by name. */
  secret?: string | undefined;
}

export interface OnboardingSearch extends CodeSearch {
  /** The factory step 3 created, by id: steps 4 and on work on it. */
  factory?: string | undefined;
}

const idPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const maxUrl = 300;

const text = (v: unknown): string | undefined => (typeof v === 'string' && v !== '' ? v : undefined);

/** The address as typed when it is an https URL with a host and no sign-in in it; else undefined. */
export function repositoryUrl(value: string | undefined): string | undefined {
  const raw = value?.trim();
  if (!raw || raw.length > maxUrl) return undefined;
  try {
    const url = new URL(raw);
    if (url.protocol !== 'https:' || url.hostname === '' || url.username !== '' || url.password !== '') return undefined;
    return raw;
  } catch {
    return undefined;
  }
}

/** "acme/app" from https://github.com/acme/app(.git); undefined when the path has no owner and name. */
export function repoOf(url: string | undefined): string | undefined {
  const valid = repositoryUrl(url);
  if (!valid) return undefined;
  const [owner, name] = new URL(valid).pathname.split('/').filter(Boolean);
  const repo = name?.replace(/\.git$/, '');
  return owner && repo ? `${owner}/${repo}` : undefined;
}

export function codeSearch(search: Record<string, unknown>): CodeSearch {
  const url = repositoryUrl(text(search.url));
  const secret = text(search.secret);
  // Every key is set, undefined when refused: the router would otherwise show the raw URL value through.
  return { url, secret: secret && secretNamePattern.test(secret) ? secret : undefined };
}

/** The URL state of every onboarding step: the Code step's choice and the factory, when there is one. */
export function onboardingSearch(search: Record<string, unknown>): OnboardingSearch {
  const factory = text(search.factory);
  return { ...codeSearch(search), factory: factory && idPattern.test(factory) ? factory : undefined };
}
