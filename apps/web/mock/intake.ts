// The intake the mock serves (ADR 0026, GET …/factories/{factory}/intake). The software factory has the
// picture's sources and its five triggers that the API can express: the picture's Slack and Sentry rows have
// no source in the contract, so they are not here. Every other factory has no items, and its triggers are
// read from its current version's factory.yaml (the onboarding's starter triggers come that way).
import { parse } from 'yaml';
import type { Intake, IntakeTrigger } from '../src/api/types.ts';

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

/** The mock's view of one trigger in factory.yaml: only the keys the intake reads. */
interface FileTrigger {
  name: string;
  enabled?: boolean;
  on: string;
  cron?: string;
  timeZone?: string;
  create: { type: string; station: string; expedite?: boolean };
}

/**
 * The triggers of a factory.yaml, as getIntake shows them. A trigger is enabled unless the file says
 * `enabled: false`. A github or webhook trigger has a missing_connection problem: the mock's factories have
 * no webhook endpoints yet, so every such trigger needs one (the contract's rule, in its simplest form).
 */
function triggersOf(yamlText: string): IntakeTrigger[] {
  const list = (parse(yamlText) as { spec?: { board?: { triggers?: FileTrigger[] } } } | null)?.spec?.board?.triggers;
  if (!Array.isArray(list)) return [];
  return list.map((t) => {
    const source: IntakeTrigger['source'] = t.on.startsWith('github.')
      ? 'github'
      : t.on.startsWith('webhook.')
        ? 'webhook'
        : 'schedule';
    const needsEndpoint = source === 'github' || source === 'webhook';
    return {
      name: t.name,
      enabled: t.enabled ?? true,
      source,
      on: t.on,
      when: source === 'schedule' ? `${t.cron ?? ''}, ${t.timeZone ?? 'UTC'}` : t.on,
      create: { type: t.create.type, station: t.create.station, lane: null, expedite: t.create.expedite ?? false },
      days: [0, 0, 0, 0, 0, 0, 0],
      problem: needsEndpoint
        ? {
            kind: 'missing_connection',
            message: `No ${source === 'github' ? 'GitHub' : 'webhook'} endpoint for this trigger.`,
          }
        : null,
    };
  });
}

/** The intake of the factory with this name, from its current version's factory.yaml when it has no fixture. */
export function intakeOf(factory: string, yamlText: string): Intake {
  if (factory === 'software') return software;
  return { items: 0, from: null, to: null, sources: none, triggers: triggersOf(yamlText) };
}
