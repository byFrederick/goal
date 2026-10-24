// The builder routes (D §8) over client.ts's goald client: the editor's metadata (models, which
// later phases are wired, the skill limits) and the skills.sh skills catalog the operator synced
// into the bucket. Every function takes the organization (slug or id) first and throws ApiError:
// 404 `skill-not-found`, 503 `catalog-unavailable`, 400 for a limit outside 1 to 50.
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { BuilderMeta, SkillFiles, SkillPage } from './types';

export function getBuilderMeta(org: string, signal?: AbortSignal): Promise<BuilderMeta> {
  return unwrap(goal.GET(orgPath('/builder/meta'), { params: { path: { org } }, ...cancel(signal) }));
}

export interface SkillSearch {
  /** Matches the key, the skill's name and its description (case-insensitive). */
  q?: string | undefined;
  /** 1 to 50; goald's default is 20. */
  limit?: number | undefined;
  /** The previous page's next_after. */
  after?: string | undefined;
}

/** One page of the catalog, most installed first. */
export function searchSkills(
  org: string,
  { q, limit, after }: SkillSearch = {},
  signal?: AbortSignal,
): Promise<SkillPage> {
  return unwrap(
    goal.GET(orgPath('/builder/skills'), {
      params: {
        path: { org },
        query: { ...(q ? { q } : {}), ...(limit !== undefined ? { limit } : {}), ...(after ? { after } : {}) },
      },
      ...cancel(signal),
    }),
  );
}

/** A catalog skill's key, `<owner>/<repo>/<skill>` (SkillListing.key). */
export type SkillKey = `${string}/${string}/${string}`;

/** Splits a skill key into its parts; undefined when it is not one. */
export function skillKeyParts(key: string): { owner: string; repo: string; skill: string } | undefined {
  const [owner, repo, skill, ...rest] = key.split('/');
  if (!owner || !repo || !skill || rest.length > 0) return undefined;
  return { owner, repo, skill };
}

/** A catalog skill's folder at the commit it is pinned at: what the Add dialog shows and vendors. */
export function getSkillFiles(org: string, key: SkillKey, signal?: AbortSignal): Promise<SkillFiles> {
  const parts = skillKeyParts(key);
  if (!parts) return Promise.reject(new Error(`${key} is not <owner>/<repo>/<skill>`));
  return unwrap(
    goal.GET(orgPath('/builder/skills/{owner}/{repo}/{skill}/files'), {
      params: { path: { org, ...parts } },
      ...cancel(signal),
    }),
  );
}
