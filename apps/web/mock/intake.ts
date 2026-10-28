// The intake the mock serves (ADR 0026, GET …/factories/{factory}/intake). The software factory has the
// picture's sources and its five triggers that the API can express: the picture's Slack and Sentry rows have
// no source in the contract, so they are not here. Every other factory has no items and no triggers, which
// shows the page's empty state.
import type { Intake } from '../src/api/types.ts';

const none = { github: 0, people: 0, cli: 0, api: 0, schedule: 0, webhook: 0 };

const software: Intake = {
  items: 48,
  from: '2026-10-20',
  to: '2026-10-26',
  sources: { github: 22, people: 11, cli: 0, api: 0, schedule: 7, webhook: 2 },
  triggers: [
    {
      name: 'issues-labeled-factory',
      enabled: true,
      source: 'github',
      on: 'github.issues.labeled',
      when: 'github.issues.labeled',
      create: { type: 'feature', station: 'Spec', lane: null, expedite: false },
      days: [1, 3, 2, 4, 2, 1, 1],
      problem: null,
    },
    {
      name: 'flaky-test-on-main',
      enabled: true,
      source: 'github',
      on: 'github.check_run.failed',
      when: 'github.check_run.failed',
      create: { type: 'fix', station: 'Build', lane: null, expedite: true },
      days: [0, 1, 0, 0, 1, 0, 1],
      problem: null,
    },
    {
      name: 'dependency-audit',
      enabled: true,
      source: 'schedule',
      on: 'schedule',
      when: '0 9 * * MON, UTC',
      create: { type: 'infra', station: 'Spec', lane: null, expedite: false },
      days: [0, 0, 0, 0, 0, 0, 1],
      problem: null,
    },
    {
      name: 'architecture-check',
      enabled: true,
      source: 'schedule',
      on: 'schedule',
      when: '0 16 * * FRI, UTC',
      create: { type: 'refactor', station: 'Spec', lane: null, expedite: false },
      days: [0, 0, 0, 0, 1, 0, 0],
      problem: null,
    },
    {
      name: 'deploy-failed',
      enabled: true,
      source: 'webhook',
      on: 'webhook.cloudflare.deploy.failed',
      when: 'webhook.cloudflare.deploy.failed',
      create: { type: 'fix', station: 'Spec', lane: null, expedite: true },
      days: [0, 0, 0, 0, 0, 0, 0],
      problem: { kind: 'refused_deliveries', message: '2 events refused, bad signature' },
    },
  ],
};

/** The intake of the factory with this name (the mock's factories are named as the fixtures name them). */
export function intakeOf(factory: string): Intake {
  if (factory === 'software') return software;
  return { items: 0, from: null, to: null, sources: none, triggers: [] };
}
