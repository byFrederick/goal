// The wizard's Start step as pure functions (D §10.11): the name a new factory is offered, the
// request that creates it, the words of a template's detail, and what an Import YAML check means
// for Continue.
import type { DefinitionProblem, FileMap, NewFactoryRequest, Validation, VersionSource } from '../../../api/types';
import { humanize } from '../../../shared/format';

/** The four ways to start, as the Factories home's cards name them (`?from=`). */
export type StartKind = 'blank' | 'template' | 'clone' | 'yaml';

export const startKinds: readonly StartKind[] = ['blank', 'template', 'clone', 'yaml'];

/** The catalog template a blank start uses when the catalog has one (B's `blank`); not listed with the others. */
export const blankTemplate = 'blank';

/** A factory's name: the schema's slug (metadata.name). */
const namePattern = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;
const nameMax = 40;

/** Why `name` cannot name a factory, or null when it can. */
export function nameProblem(name: string): string | null {
  if (namePattern.test(name)) return null;
  return 'Use 3 to 40 lowercase letters, digits and dashes, starting with a letter and not ending with a dash.';
}

/** `base` when no factory of the organization has it, else `base-2`, `base-3`, … (kept within 40 characters). */
export function freeName(base: string, taken: readonly string[]): string {
  const used = new Set(taken);
  const stem = base.slice(0, nameMax).replace(/-+$/, '');
  if (!used.has(stem)) return stem;
  for (let n = 2; ; n++) {
    const suffix = `-${n}`;
    const name = `${stem.slice(0, nameMax - suffix.length).replace(/-+$/, '')}${suffix}`;
    if (!used.has(name)) return name;
  }
}

/** What the name field offers before the person types one; an import uses factory.yaml's name. */
export function suggestedName(
  kind: StartKind,
  picked: { template?: string | undefined; clone?: string | undefined },
  taken: readonly string[],
): string {
  switch (kind) {
    case 'blank':
      return freeName('new-factory', taken);
    case 'template':
      return picked.template ? freeName(picked.template, taken) : '';
    case 'clone':
      return picked.clone ? freeName(`${picked.clone}-copy`, taken) : '';
    case 'yaml':
      return '';
  }
}

export interface StartChoice {
  kind: StartKind;
  name: string;
  template?: string | undefined;
  /** The factory to copy: its id. */
  clone?: string | undefined;
  files?: FileMap | undefined;
}

/** The POST /factories body for a choice; null while the choice is incomplete. */
export function newFactoryRequest(choice: StartChoice): NewFactoryRequest | null {
  const name = choice.name.trim();
  switch (choice.kind) {
    case 'blank':
      return name ? { name, start: { kind: 'blank' } } : null;
    case 'template':
      return name && choice.template ? { name, start: { kind: 'template', template: choice.template } } : null;
    case 'clone':
      return name && choice.clone ? { name, start: { kind: 'clone', factory: choice.clone } } : null;
    case 'yaml':
      return choice.files ? { ...(name ? { name } : {}), start: { kind: 'import', files: choice.files } } : null;
  }
}

/** "cost-analyst reads the cost report, proposer drafts the fix and reviewer checks it." */
export function agentsSentence(agents: readonly { name: string; description?: string | undefined }[]): string {
  if (agents.length === 0) return 'No agents yet.';
  const parts = agents.map((a) => {
    const role = (a.description ?? '').trim().replace(/\.$/, '');
    return role ? `${a.name} ${role.charAt(0).toLowerCase()}${role.slice(1)}` : a.name;
  });
  const last = parts.pop() as string;
  return `${parts.length > 0 ? `${parts.join(', ')} and ${last}` : last}.`;
}

/** A template's line as chips: each station's name, and whether a person decides there. */
export function lineChips(
  stations: readonly { id: string; name?: string | undefined; kind?: string | undefined }[],
): { id: string; label: string; human: boolean }[] {
  return stations.map((s) => ({ id: s.id, label: s.name || humanize(s.id), human: s.kind === 'human' }));
}

/** The checks that must pass before Continue: the file parses and matches the schema. */
const gateChecks = new Set(['yaml', 'schema']);

export interface ImportVerdict {
  /** It parses and passes the schema: Continue may create the factory. */
  ok: boolean;
  /** Errors that block Continue, by line. */
  blocking: DefinitionProblem[];
  /** The name is another factory's: the name field says so. */
  nameTaken: DefinitionProblem | undefined;
  /** Everything else: fixed in the next steps. */
  later: DefinitionProblem[];
}

/** Where a new factory's draft came from, under the wizard's heading: "From the Cost review template." */
export function sourceSentence(
  source: VersionSource | undefined,
  names: { templateTitle?: string | undefined; factoryName?: string | undefined },
): string {
  switch (source?.kind) {
    case 'template': {
      const name = source.template?.split('@')[0] ?? '';
      return `From the ${names.templateTitle ?? humanize(name)} template.`;
    }
    case 'blank':
      return 'A blank factory.';
    case 'clone': {
      const version = source.version && /^\d+$/.test(source.version) ? ` v${source.version}` : '';
      return `A copy of ${names.factoryName ?? 'another factory'}${version}.`;
    }
    case 'import':
    case 'cli':
      return 'Imported from YAML.';
    default:
      return '';
  }
}

export function importVerdict(validation: Validation): ImportVerdict {
  const blocking = validation.problems.filter((p) => p.severity === 'error' && gateChecks.has(p.check));
  const nameTaken = validation.problems.find((p) => p.code === 'name-taken');
  const later = validation.problems.filter((p) => !blocking.includes(p) && p !== nameTaken && p.severity !== 'info');
  return { ok: blocking.length === 0, blocking, nameTaken, later };
}
