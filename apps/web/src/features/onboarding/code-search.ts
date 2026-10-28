// The Code step's URL state (frontend.md: a selection lives in the URL, so a linked step shows the same
// choice): the GitHub connection, the owner whose repositories are listed, and the repositories chosen by
// full name. Malformed values are dropped, never thrown on. Step 3 reads the same search.
import { connectionName } from '../../shared/connections';

export interface CodeSearch {
  /** The connection to list repositories through, by name. Absent: the organization's first GitHub connection. */
  connection?: string | undefined;
  /** The account or organization whose repositories are listed. Absent: the connection's own account. */
  owner?: string | undefined;
  /** The chosen repositories, as `<owner>/<name>`. */
  repos?: string[] | undefined;
}

const ownerPattern = /^[A-Za-z0-9][A-Za-z0-9-]{0,98}$/;
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
