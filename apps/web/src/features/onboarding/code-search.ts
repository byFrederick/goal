// The onboarding steps' URL state (frontend.md: a selection lives in the URL, so a linked step shows the same
// choice). From step 2 on the factory made there (`factory`, its id) and its template ride along. After the
// Code step, `url` (the first repository, or an owner pattern) and `secret` (the token's name) name the code
// the preview shows; the token itself is never in the URL. Malformed values are dropped, never thrown on.
import { secretNamePattern } from '../secrets/words';
import type { CodeAccess } from './code-repositories';

export interface CodeSearch {
  /** The first repository's https URL, such as https://github.com/acme/app, or an owner pattern: the later steps' card names it. */
  url?: string | undefined;
  /** The secret that holds the token, by name. */
  secret?: string | undefined;
  /** The access chosen on the Code step, so Back and a reload show the same choice. */
  access?: CodeAccess | undefined;
}

export interface OnboardingSearch extends CodeSearch {
  /** The factory step 2 created, by id: every later step works on it. */
  factory?: string | undefined;
  /** The template it came from, by name: the Code step reads the code its line needs. Absent for a blank start. */
  template?: string | undefined;
}

const idPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const maxUrl = 300;
const maxTemplate = 100;

const text = (v: unknown): string | undefined => (typeof v === 'string' && v !== '' ? v : undefined);

/** The address as typed when it is an https URL with a host and no sign-in in it; else undefined. */
export function repositoryUrl(value: string | undefined): string | undefined {
  const raw = value?.trim();
  if (!raw || raw.length > maxUrl) return undefined;
  try {
    const url = new URL(raw);
    if (url.protocol !== 'https:' || url.hostname === '' || url.username !== '' || url.password !== '')
      return undefined;
    return raw;
  } catch {
    return undefined;
  }
}

/** "acme/app" from https://github.com/acme/app(.git), "acme/*" from an owner pattern; undefined when the path has no owner and name. */
export function repoOf(url: string | undefined): string | undefined {
  const valid = repositoryUrl(url);
  if (!valid) return undefined;
  const [owner, name] = new URL(valid).pathname.split('/').filter(Boolean);
  const repo = name === '*' ? '*' : name?.replace(/\.git$/, '');
  return owner && repo ? `${owner}/${repo}` : undefined;
}

export function codeSearch(search: Record<string, unknown>): CodeSearch {
  const url = repositoryUrl(text(search.url));
  const secret = text(search.secret);
  const access =
    search.access === 'none' || search.access === 'read' || search.access === 'write' ? search.access : undefined;
  // Every key is set, undefined when refused: the router would otherwise show the raw URL value through.
  return { url, secret: secret && secretNamePattern.test(secret) ? secret : undefined, access };
}

/** The URL state of every onboarding step: the Code step's choice and the factory, when there is one. */
export function onboardingSearch(search: Record<string, unknown>): OnboardingSearch {
  const factory = text(search.factory);
  const template = text(search.template);
  return {
    ...codeSearch(search),
    factory: factory && idPattern.test(factory) ? factory : undefined,
    template: template && template.length <= maxTemplate ? template : undefined,
  };
}
