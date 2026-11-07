import type { Run } from '@goal/api-client';
import type { MockItem } from './fixtures.ts';

// The starter item of the mock's software-delivery template (ADR 0032 decision 3): the template's YAML carries it as
// `starter.item`, and the mock's submit answers for it as the picture shows it (api.ts progressStarter).
export const starterItem = {
  title: 'Add a short README section on how to run the tests',
  type: 'feature',
  body: 'Add a "Running the tests" section to the README that says which command runs the tests and what it needs installed. Change nothing else.',
};

/** The incident-response template's starter item, as deploy/catalog/templates commits it. */
export const incidentStarterItem = {
  title: 'Checkout page returns errors for some visitors',
  type: 'incident',
  body: 'Since this morning, some visitors to https://shop.example.com/checkout see an error page. Sort it by severity, find the likely cause and propose a fix.',
};

/** The support-replies template's starter item, as deploy/catalog/templates commits it. */
export const supportStarterItem = {
  title: 'How do I reset my password?',
  type: 'ticket',
  body: 'A customer writes from jo@example.com: "I forgot my password and the reset email never arrives. How do I get back into my account?"',
};

/** The code-review template's starter item: the mock's submit answers for it the same way (api.ts). */
export const reviewStarterItem = {
  title: 'Review the newest open pull request',
  type: 'review',
  body: 'Read the newest open pull request against the team conventions and say what to change, in two or three points.',
};

/** Every title the mock treats as a starter item: it runs its whole course at once. */
export const starterTitles: readonly string[] = [
  starterItem.title,
  reviewStarterItem.title,
  incidentStarterItem.title,
  supportStarterItem.title,
];

/** What the starter course needs from the mock: the item, its event log and the runs list. */
export interface StarterCourse {
  item: MockItem;
  runs: Run[];
  emit: (it: MockItem, kind: string, actor: string, data?: Record<string, unknown>, at?: string) => void;
  /** The mock's clock, in ms. */
  now: number;
  /** Whether the line's published version sets runtime.modelKey. */
  keyed: boolean;
  /** The agent that claims the item. */
  agent: string;
  /** The published version the run ran on. */
  version: { id: string; number: number | null } | null;
}

/**
 * The mock runs no agent, so a starter item runs its course at once, as goald would. Without a model key on the
 * line it is claimed and blocked with the code model_key_missing, and has no run. With one it is created nine
 * minutes ago, its spec accepted, moved to Build, claimed, run and merged at Done, with $0.84 spent. Seconds from
 * its creation. The Floor then hides the new-factory checklist, as it does once an item has merged
 * (frontend.md, "New factory row").
 */
export function runStarterCourse({ item: it, runs, emit, now, keyed, agent, version }: StarterCourse): void {
  const at = (s: number) => new Date(now - (540 - s) * 1000).toISOString();
  it.created_at = at(0);
  emit(it, 'created', 'engine', {}, at(0));
  if (!keyed) {
    emit(it, 'claimed', 'engine', { slot: 1, agent }, at(12));
    emit(
      it,
      'blocked',
      'engine',
      { code: 'model_key_missing', reason: 'The line has no model key, so no agent can run.' },
      at(13),
    );
    it.state = 'blocked';
    return;
  }
  it.station = 'spec';
  emit(it, 'moved', 'engine', { from: 'inbox', to: 'spec' }, at(12));
  emit(it, 'approved', 'user:local', {}, at(270));
  it.station = 'build';
  emit(it, 'moved', 'engine', { from: 'spec', to: 'build' }, at(302));
  const run = `${it.id.slice(-6)}-run-1`;
  emit(it, 'claimed', 'engine', { slot: 1, agent }, at(305));
  emit(it, 'run_started', agent, {}, at(306));
  runs.push({
    id: run,
    item_id: it.id,
    station: 'build',
    agent,
    model: 'claude-sonnet-4-5',
    status: 'succeeded',
    turns: 12,
    input_tokens: 48_000,
    output_tokens: 7_200,
    cost_usd: 0.84,
    error: null,
    started_at: at(306),
    ended_at: at(539),
    cpu_seconds: 186,
    peak_memory_bytes: 812 * 1024 ** 2,
    box_size: 'medium',
    definition_version_id: version?.id ?? null,
    definition_version: version?.number ?? null,
  });
  it.station = 'done';
  emit(it, 'moved', 'engine', { from: 'build', to: 'done' }, at(540));
  it.state = 'done';
  it.cost_usd = 0.84;
}
