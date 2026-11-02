// GET /v1/orgs/{org}/items/{item}/teardown (ADR 0019, decision 3), worked out from the item's spans, runs and events
// as goald works it out on each read, simplified: working is time in an in_progress span, waiting any other span;
// a loop lasts until the next run at its station ends and costs that run.
import type { ItemEvent, ItemSpan, Run, Teardown, TeardownRun, Touch } from '@goal/api-client';

export interface TeardownInput {
  spans: readonly ItemSpan[];
  runs: readonly Run[];
  events: readonly ItemEvent[];
  touches: readonly Touch[];
  done: boolean;
  /** The item's last run ends in the merge when it is done. */
  now: number;
  targetSeconds: number | null;
  budgetUsd: number | null;
}

const seconds = (ms: number) => Math.max(0, Math.round(ms / 1000));

const outcomeOf = (run: Run, last: boolean, done: boolean, failedAfter: boolean): TeardownRun['outcome'] => {
  if (run.status === 'running') return 'running';
  if (run.status === 'failed') return 'failed';
  if (run.status === 'cancelled') return 'cancelled';
  if (failedAfter) return 'check_failed';
  return last && done ? 'merged' : 'finished';
};

const causes: Record<string, 'check_failed' | 'gate_rejected' | 'rejected'> = {
  check_failed: 'check_failed',
  gate_rejected: 'gate_rejected',
  rejected: 'rejected',
};

export function teardownOf(input: TeardownInput): Teardown {
  const { spans, runs, events, touches, done, now } = input;
  const stations: Teardown['stations'] = [];
  for (const s of spans) {
    const length = seconds(Date.parse(s.left_at ?? new Date(now).toISOString()) - Date.parse(s.entered_at));
    let row = stations.find((r) => r.station === s.station);
    if (!row) {
      row = { station: s.station, working_seconds: 0, waiting_seconds: 0, total_seconds: 0 };
      stations.push(row);
    }
    if (s.state === 'in_progress') row.working_seconds += length;
    else row.waiting_seconds += length;
    row.total_seconds += length;
  }
  const total = stations.reduce(
    (t, r) => ({
      working_seconds: t.working_seconds + r.working_seconds,
      waiting_seconds: t.waiting_seconds + r.waiting_seconds,
      total_seconds: t.total_seconds + r.total_seconds,
    }),
    { working_seconds: 0, waiting_seconds: 0, total_seconds: 0 },
  );

  const failures = events.filter((e) => e.kind in causes);
  const rows: TeardownRun[] = runs.map((r, i) => ({
    id: r.id,
    station: r.station,
    agent: r.agent,
    model: r.model,
    turns: r.turns,
    seconds: seconds(Date.parse(r.ended_at ?? new Date(now).toISOString()) - Date.parse(r.started_at)),
    cost_usd: r.cost_usd,
    status: r.status,
    outcome: outcomeOf(
      r,
      i === runs.length - 1,
      done,
      // A failed check follows the run whose output it checked: the last run at its station before the event.
      failures.some(
        (e) =>
          e.kind === 'check_failed' &&
          runs.findLast((x) => x.station === e.station && x.started_at <= e.at)?.id === r.id,
      ),
    ),
  }));

  const loops: Teardown['loops'] = failures.map((e) => {
    const next = runs.find((r) => r.station === e.station && r.started_at >= e.at);
    const reason = e.data.reason ?? e.data.command;
    return {
      cause: causes[e.kind] ?? 'check_failed',
      station: e.station ?? '',
      ...(typeof e.data.to === 'string' ? { to: e.data.to } : {}),
      ...(typeof reason === 'string' ? { reason } : {}),
      at: e.at,
      seconds: next ? seconds(Date.parse(next.ended_at ?? new Date(now).toISOString()) - Date.parse(e.at)) : 0,
      cost_usd: next?.cost_usd ?? 0,
      event_id: e.id,
    };
  });

  const waits: Teardown['touches'] = touches.map((t) => {
    const before = events.findLast((e) => e.at < t.at && e.id < t.event_id);
    return { touch: t, wait_seconds: before ? seconds(Date.parse(t.at) - Date.parse(before.at)) : 0 };
  });

  return {
    open: !done,
    lead_time_seconds: total.total_seconds,
    ...(input.targetSeconds ? { target_seconds: input.targetSeconds } : {}),
    cost_usd: rows.reduce((n, r) => n + r.cost_usd, 0),
    ...(input.budgetUsd ? { budget_usd: input.budgetUsd } : {}),
    rework: {
      loops: loops.length,
      seconds: loops.reduce((n, l) => n + l.seconds, 0),
      cost_usd: loops.reduce((n, l) => n + l.cost_usd, 0),
    },
    touch_count: touches.length,
    touch_wait_seconds: waits.reduce((n, w) => n + w.wait_seconds, 0),
    stations,
    total,
    runs: rows,
    run_total: {
      runs: rows.length,
      turns: rows.reduce((n, r) => n + r.turns, 0),
      seconds: rows.reduce((n, r) => n + r.seconds, 0),
      cost_usd: rows.reduce((n, r) => n + r.cost_usd, 0),
    },
    loops,
    touches: waits,
  };
}
