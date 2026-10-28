// The Code step's URL state (frontend.md: a selection lives in the URL, so a linked step shows the same
// choice): the GitHub connection, the owner whose repositories are listed, and the repositories chosen by
// full name. Malformed values are dropped, never thrown on. Steps 3 and 4 read the same search, and
// from step 3 on the factory created there (`factory`, its id) rides along.
import { connectionName } from '../../shared/connections';

export interface CodeSearch {
  /** The connection to list repositories through, by name. Absent: the organization's first GitHub connection. */
  connection?: string | undefined;
  /** The account or organization whose repositories are listed. Absent: the connection's own account. */
  owner?: string | undefined;
  /** The chosen repositories, as `<owner>/<name>`. */
  repos?: string[] | undefined;
}

export interface OnboardingSearch extends CodeSearch {
  /** The factory step 3 created, by id: steps 4 and on work on it. */
  factory?: string | undefined;
}

const ownerPattern = /^[A-Za-z0-9][A-Za-z0-9-]{0,98}$/;
const idPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const repoPattern = /^[A-Za-z0-9._-]+\/[A-Za-z0-9._-]+$/;
/** The most repositories one list answers, so no more can be chosen. */
const maxRepos = 100;

const text = (v: unknown): string | undefined => (typeof v === 'string' && v !== '' ? v : undefined);

export function codeSearch(search: Record<string, unknown>): CodeSearch {
  const connection = text(search.connection);
  const owner = text(search.owner);
  const repos = [...new Set([search.repos].flat().filter((r): r is string => typeof r === 'string'))]
    .filter((r) => repoPattern.test(r))
    .slice(0, maxRepos);
  // Every key is set, undefined when refused: the router would otherwise show the raw URL value through.
  return {
    connection: connection && connectionName.test(connection) ? connection : undefined,
    owner: owner && ownerPattern.test(owner) ? owner : undefined,
    repos: repos.length > 0 ? repos : undefined,
  };
}

/** The URL state of every onboarding step: the Code step's choice and the factory, when there is one. */
export function onboardingSearch(search: Record<string, unknown>): OnboardingSearch {
  const factory = text(search.factory);
  return { ...codeSearch(search), factory: factory && idPattern.test(factory) ? factory : undefined };
}
