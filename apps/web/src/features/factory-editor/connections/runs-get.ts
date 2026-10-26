// The builder's "Runs get" column (canvas Define-Connections): a connection's first variable and
// how many more, or for a kind whose credentials are minted per run (AWS) their shared prefix,
// "AWS_*", since the values are made for each run.
import type { ConnectionKind } from '../../../api/types';

export interface RunsGetSummary {
  names: string[];
  more: number;
  minted: boolean;
}

export function runsGetSummary(kind: ConnectionKind | undefined, env: string[]): RunsGetSummary {
  if (env.length === 0) return { names: [], more: 0, minted: false };
  if (kind?.mint) {
    const prefix = env[0]?.split('_')[0] ?? '';
    const shared = prefix !== '' && env.every((e) => e.startsWith(`${prefix}_`));
    if (shared) return { names: [`${prefix}_*`], more: 0, minted: true };
    return { names: env.slice(0, 1), more: env.length - 1, minted: true };
  }
  return { names: env.slice(0, 1), more: env.length - 1, minted: false };
}
