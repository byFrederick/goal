// Fault injection for the e2e tests: POST /__mock/fail {path, method?, status?, code?, times?,
// delay_ms?, problems?} makes the next `times` requests (1 by default, 0 for every one until a
// reset) whose path contains `path` (and whose method is `method`, when given) wait `delay_ms`,
// then answer `status` instead of being served. Without a status the request is only delayed.
// DELETE /__mock/fail clears them; POST /__mock/reset does too.
import type { DefinitionProblem } from '@goal/api-client';

export interface MockFault {
  path: string;
  method?: string;
  status?: number;
  code?: string;
  /** Requests left; 0 is every request until a reset. */
  times: number;
  delay_ms?: number;
  /** The problems of a 409 definition-invalid; the draft's own when absent. */
  problems?: DefinitionProblem[];
}

/** A fault from a request body; undefined when it names no path. */
export function parseFault(body: Record<string, unknown>): MockFault | undefined {
  if (typeof body.path !== 'string' || body.path === '') return undefined;
  const int = (v: unknown) => (Number.isInteger(v) ? (v as number) : undefined);
  const fault: MockFault = { path: body.path, times: int(body.times) ?? 1 };
  if (typeof body.method === 'string') fault.method = body.method.toUpperCase();
  const status = int(body.status);
  if (status !== undefined) fault.status = status;
  if (typeof body.code === 'string') fault.code = body.code;
  const delay = int(body.delay_ms);
  if (delay !== undefined) fault.delay_ms = delay;
  if (Array.isArray(body.problems)) fault.problems = body.problems as DefinitionProblem[];
  return fault;
}

/** The first fault matching the request, counted down (and dropped when used up). */
export function takeFault(faults: MockFault[], method: string, path: string): MockFault | undefined {
  const i = faults.findIndex((f) => path.includes(f.path) && (!f.method || f.method === method));
  const fault = faults[i];
  if (!fault) return undefined;
  if (fault.times > 0) {
    fault.times -= 1;
    if (fault.times === 0) faults.splice(i, 1);
  }
  return fault;
}

export const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));
