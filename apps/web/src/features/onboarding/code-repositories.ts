// The Code step's repositories as factory.yaml's `spec.runtime.repositories` entries (ADR 0041 decision 9).
// The draft model has no such section, so the step edits the file's text with the yaml Document API: comments
// and the order of every other key stay as they are.
import { parseDocument } from 'yaml';
import { repositoryUrl } from './code-search';

/** What the factory's agents may do with the code. `none` writes no entries. */
export type CodeAccess = 'none' | 'read' | 'write';

/** One `runtime.repositories` entry as the step writes it. */
export interface RepositoryEntry {
  name: string;
  url: string;
  token: string;
  access: 'read' | 'write';
  default?: true;
}

const slug = (value: string) =>
  value
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, '-')
    .replace(/^-+|-+$/g, '');

/** The https URL that covers every repository of an owner: https://github.com/acme/*. */
export function ownerPattern(host: string, owner: string): string {
  return `https://${host.trim()}/${owner.trim().replace(/^\/+|\/+$/g, '')}/*`;
}

/** The URL's path pieces: the owner, and the repository (undefined for an owner pattern). */
function parts(url: string): { owner: string; repo: string | undefined } {
  const [owner = '', name] = new URL(url).pathname.split('/').filter(Boolean);
  return { owner, repo: name === undefined || name === '*' ? undefined : name.replace(/\.git$/, '') };
}

/** What the entry says in the preview and the results: acme/app, or acme/* for a whole organization. */
export function repositoryLabel(url: string): string {
  const valid = repositoryUrl(url);
  if (!valid) return url;
  const { owner, repo } = parts(valid);
  return `${owner}/${repo ?? '*'}`;
}

/**
 * The entries for the chosen URLs: a name from each URL (unique in the list), the token's secret reference
 * and the access. The default is set when exactly one entry names one repository.
 */
export function repositoryEntries(
  urls: readonly string[],
  secret: string,
  access: 'read' | 'write',
): RepositoryEntry[] {
  const used = new Set<string>();
  const entries = urls.map((url): RepositoryEntry => {
    const { owner, repo } = parts(url);
    const base = slug(repo ?? owner) || 'repo';
    let name = base;
    for (let n = 2; used.has(name); n++) name = `${base}-${n}`;
    used.add(name);
    return { name, url, token: `{{${secret}}}`, access };
  });
  const [only] = entries;
  if (entries.length === 1 && only && parts(only.url).repo !== undefined) only.default = true;
  return entries;
}

/**
 * factory.yaml's text with `spec.runtime.repositories` set to the entries and the `runtime.repository`
 * shorthand removed. No entries leaves the list out. A file without a `runtime` map is returned as it is.
 */
export function withRepositories(text: string, entries: readonly RepositoryEntry[]): string {
  const doc = parseDocument(text);
  if (doc.getIn(['spec', 'runtime']) === undefined) return text;
  doc.deleteIn(['spec', 'runtime', 'repository']);
  if (entries.length === 0) doc.deleteIn(['spec', 'runtime', 'repositories']);
  else doc.setIn(['spec', 'runtime', 'repositories'], doc.createNode(entries));
  return String(doc);
}

/**
 * The owner pattern for what is typed in "A whole organization": https://github.com/acme, with or without a
 * trailing /*. Undefined unless it is an https address with one path segment.
 */
export function ownerUrl(input: string): string | undefined {
  const valid = repositoryUrl(input.trim().replace(/\/\*$/, ''));
  if (!valid) return undefined;
  const url = new URL(valid);
  const segments = url.pathname.split('/').filter(Boolean);
  const [owner] = segments;
  return segments.length === 1 && owner ? ownerPattern(url.host, owner) : undefined;
}

/** A secret name for a token for these repositories: github-acme from https://github.com/acme/app. */
export function suggestedSecret(url: string | undefined): string {
  const valid = repositoryUrl(url);
  if (!valid) return 'code-token';
  const { owner } = parts(valid);
  const name = slug(`${new URL(valid).hostname.split('.')[0]}-${owner}`)
    .slice(0, 39)
    .replace(/-+$/, '');
  return /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/.test(name) ? name : 'code-token';
}
