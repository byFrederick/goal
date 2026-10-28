// The shell's search for the mock API (GET /v1/orgs/{org}/search, openapi.yaml searchOrg): each group
// ranked as goald ranks it, an exact match first, then a prefix, then the rest of a substring match,
// up to limit results each. An item matches by its title, or by its exact number with or without the
// "#". Pages are not here: the shell matches them from the screens registry.
import type { AgentHit, ConnectionHit, FactoryHit, OrgSearch, SearchItem } from '@goal/api-client';

export interface SearchSource {
  items: SearchItem[];
  agents: AgentHit[];
  connections: ConnectionHit[];
  factories: FactoryHit[];
}

/** 0 for an exact match, 1 for a prefix, 2 for the rest of a substring match, undefined for none. */
function rank(text: string, needle: string): number | undefined {
  const t = text.toLowerCase();
  if (t === needle) return 0;
  if (t.startsWith(needle)) return 1;
  if (t.includes(needle)) return 2;
  return undefined;
}

/** The best rank of the texts that match, so a factory matches by its name or its title. */
function bestRank(texts: string[], needle: string): number | undefined {
  const ranks = texts.flatMap((t) => rank(t, needle) ?? []);
  return ranks.length === 0 ? undefined : Math.min(...ranks);
}

/** The first `limit` of `list` that rank, best first; ties keep the order the list has. */
function top<T>(list: T[], limit: number, rankOf: (value: T) => number | undefined): T[] {
  return list
    .flatMap((value, i) => {
      const r = rankOf(value);
      return r === undefined ? [] : [{ value, r, i }];
    })
    .sort((a, b) => a.r - b.r || a.i - b.i)
    .slice(0, limit)
    .map((entry) => entry.value);
}

/** What the search finds for `q` (already trimmed) in `source`, each group up to `limit` results. */
export function search(source: SearchSource, q: string, limit: number): OrgSearch {
  const needle = q.toLowerCase();
  const number = /^#?(\d+)$/.exec(needle)?.[1];
  return {
    items: top(source.items, limit, (item) => {
      if (number !== undefined && Number(number) === item.number) return 0;
      return rank(item.title, needle);
    }),
    agents: top(source.agents, limit, (agent) => rank(agent.name, needle)),
    factories: top(source.factories, limit, (factory) => bestRank([factory.name, factory.title], needle)),
    connections: top(source.connections, limit, (connection) => rank(connection.name, needle)),
  };
}
