// What the Line section says about a station, in words (D §10.6): its name, the intake summary
// of a queue, the line preview's subtitles, the rework note and the buffer sentence. Pure, so the
// section and its tests read the same words.
import type { LineSection, ModelStation, StationCheck } from '../../../shared/definition/model';

export const stationLabel = (s: ModelStation): string => s.name || s.id;

export const kindLabel: Record<ModelStation['kind'], string> = {
  queue: 'Queue',
  agent: 'Agent',
  human: 'Human',
  done: 'Done',
};

const text = (v: unknown) => (typeof v === 'string' && v !== '' ? v : undefined);
const list = (v: unknown) => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []);

/** One intake source as people say it ("GitHub issues labelled goal", "On a schedule"). */
function sourceLabel(source: Record<string, unknown>): string {
  const kind = text(source.source) ?? 'source';
  const labels = list(source.labels);
  if (kind.startsWith('github')) {
    return labels.length > 0 ? `GitHub issues labelled ${labels.join(', ')}` : 'GitHub issues';
  }
  if (kind === 'cron' || text(source.cron)) return 'On a schedule';
  if (kind === 'manual') return 'Manual (people and the API)';
  return kind;
}

/** Where new work comes from: "Manual (people and the API)" when the line names no source. */
export function intakeSummary(intake: LineSection['intake']): string {
  const sources = intake ?? [];
  return sources.length === 0 ? 'Manual (people and the API)' : sources.map(sourceLabel).join(' · ');
}

/** Whether the line's intake is more than manual: read-only here until connectors arrive. */
export const hasExternalIntake = (intake: LineSection['intake']) =>
  (intake ?? []).some((s) => text(s.source) !== 'manual');

/** A check as its chip says it: the command, or "<output> contract" for a schema check. */
export function checkLabel(check: string | StationCheck, output: string | undefined): string {
  if (typeof check === 'string') return check;
  if (check.kind === 'schema') return `${output ?? 'output'} contract`;
  return check.run ?? check.kind ?? 'check';
}

/** The line preview's second line for a station ("cost-analyst × 2", "reviewer, gate", "you, merge"). */
export function previewDetail(s: ModelStation, line: LineSection): string {
  const gate = s.gate?.on_reject !== undefined;
  switch (s.kind) {
    case 'queue':
      return (line.intake ?? []).length > 0 ? intakeSummary(line.intake) : 'queue';
    case 'agent': {
      const lanes = Object.keys(s.route ?? {}).length;
      const flags = `${gate ? ', gate' : ''}${s.action === 'merge' ? ', merge' : ''}`;
      if (lanes > 0) return `${lanes} by lane${flags}`;
      if (!s.agent) return 'no agent yet';
      const n = s.agents ?? 1;
      return `${s.agent}${n > 1 ? ` × ${n}` : ''}${flags}`;
    }
    case 'human':
      return ['you', s.action === 'merge' ? 'merge' : '', gate ? 'gate' : ''].filter(Boolean).join(', ');
    case 'done':
      return line.librarian ? line.librarian : 'items close';
  }
}

const times = (n: number) => (n === 1 ? 'once' : n === 2 ? 'twice' : `${n} times`);

/** "Review sends work back to Build, at most twice." for a station with a gate; undefined without. */
export function reworkSentence(s: ModelStation, stations: readonly ModelStation[]): string | undefined {
  const to = s.gate?.on_reject;
  if (to === undefined) return undefined;
  const target = stations.find((x) => x.id === to);
  const max = s.gate?.max_rework;
  const back = `${stationLabel(s)} sends work back to ${target ? stationLabel(target) : to}`;
  return max ? `${back}, at most ${times(max)}.` : `${back}.`;
}

/**
 * A queue's buffer sentence: "New work enters when Spec has fewer than 2 waiting", when the line
 * releases by drum-buffer-rope and the next station keeps a buffer; undefined otherwise.
 */
export function bufferSentence(line: LineSection, id: string): string | undefined {
  const stations = line.stations ?? [];
  const at = stations.findIndex((s) => s.id === id);
  const next = stations[at + 1];
  if (line.flow?.release !== 'drum-buffer-rope' || !next?.buffer) return undefined;
  return `New work enters when ${stationLabel(next)} has fewer than ${next.buffer} waiting`;
}

/** The hint when a factory with a repository has no merging station: its pull requests stay open. */
export function noMergeHint(line: LineSection, hasRepository: boolean): string | undefined {
  if (!hasRepository || (line.stations ?? []).some((s) => s.action === 'merge')) return undefined;
  return 'No station merges, so pull requests stay open until someone merges them outside Goal.';
}

/** The stations a gate at index `at` may send work back to: those before it, but Done. */
export function upstreamOf(stations: readonly ModelStation[], at: number): ModelStation[] {
  return stations.slice(0, Math.max(0, at)).filter((s) => s.kind !== 'done');
}
