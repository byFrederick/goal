// Typed views of B's FactoryModel (the contract types every item as an open object, because the
// model mirrors factory.yaml leniently). These are the shapes the editor writes and expects; a
// half-written draft may hold other values where B reports a problem, so readers stay defensive
// (`?? []`, typeof checks). Keys are the model's snake_case keys.
import type { FactoryModel, Section } from '../../api/types';

export type { Section };

/** The sections in the editor's order. */
export const sections = ['basics', 'line', 'agents', 'connections', 'review'] as const satisfies readonly Section[];

export interface Repository {
  url?: string;
  /** The organization connection git signs in with; absent for a public repository. */
  connection?: string;
  /** Removed in phase 4 (env-reference-removed): present while a version still has it, so its fix can remove it. */
  token?: string;
}

/** Each run's box (runtime.size); medium when absent. */
export type SandboxSize = 'small' | 'medium' | 'large';

export interface BasicsSection {
  /** metadata.id, read-only. */
  id?: string;
  name?: string;
  title?: string;
  purpose?: string;
  work_types?: string[];
  /** runtime.modelConnection: the organization connection that gives every run its model. */
  model_connection?: string;
  size?: SandboxSize;
  /** runtime.tools: mise tool specs every run installs after the repository's own. */
  tools?: string[];
  repository?: Repository | null;
  /** knowledge.sources: the organization's knowledge sources the factory may search. */
  knowledge_sources?: string[];
  // Removed in phase 4 (field-removed): present while factory.yaml has them, so their fixes apply.
  runner?: string;
  image?: string;
  auth?: string;
  secrets?: Record<string, unknown>;
  knowledge_base?: Record<string, unknown>;
}

export type StationKind = 'queue' | 'agent' | 'human' | 'done';

export interface StationCheck {
  kind?: string;
  run?: string;
  min?: number;
}

export interface Gate {
  on_reject?: string;
  max_rework?: number;
}

export interface ModelStation {
  id: string;
  name?: string;
  description?: string;
  kind: StationKind;
  agent?: string;
  /** Lane → agent. */
  route?: Record<string, string>;
  /** Parallel agents (1–20). */
  agents?: number;
  wip?: number;
  buffer?: number;
  output?: string;
  checks?: (string | StationCheck)[];
  gate?: Gate;
  action?: 'merge';
}

export interface Flow {
  expedite?: { types?: string[]; wip?: number };
  constraint?: string;
  release?: string;
  andon?: { rework_rate?: number; window?: number };
  targets?: { throughput_per_day?: number; lead_time_hours?: number; expedite_lead_time_minutes?: number };
}

export interface LineSection {
  stations?: ModelStation[];
  intake?: Record<string, unknown>[];
  flow?: Flow;
  librarian?: string | null;
}

export type BuiltinMode = 'off' | 'ask' | 'allowed';

export interface Builtins {
  read?: BuiltinMode;
  edit?: BuiltinMode;
  run?: { mode: BuiltinMode; commands?: string[] };
  web?: BuiltinMode;
  subagents?: BuiltinMode;
}

export type BuiltinKey = keyof Builtins;

export interface Prompt {
  path: string;
  /** Read-only: the prompt file's sha256 and word count when it exists. */
  sha256?: string;
  words?: number;
}

export interface ModelAgent {
  name: string;
  description?: string;
  prompt?: Prompt;
  model?: string;
  max_turns?: number;
  per_item_usd?: number;
  skills?: string[];
  connections?: string[];
  builtins?: Builtins | null;
  /** Read-only: the agent has a legacy tools block (edited in YAML). */
  custom_tools?: boolean;
}

export interface ModelSkill {
  name: string;
  source: string;
  /** skills.sh:<owner>/<repo>@<skill>#<commit>, read-only. */
  origin?: string;
}

/** A factory rule (ADR 0014): its entry in the agents section; the rule's text is rules/<name>.md. */
export interface ModelRule {
  name: string;
  source: string;
}

export interface AgentsSection {
  default_model?: string;
  agents?: ModelAgent[];
  skills?: ModelSkill[];
  rules?: ModelRule[];
}

export interface ModelConnection {
  name: string;
  connector?: string;
  type?: string;
  credential?: string;
  agents?: string[];
  tools?: Record<string, Record<string, unknown>>;
  settings?: Record<string, unknown>;
  /** Read-only: the inline `type` form, whose access is each agent's `connections`. */
  legacy?: boolean;
}

export type ConnectionsSection = ModelConnection[];

export interface Budget {
  weekly_usd?: number;
  daily_usd?: number;
  per_item_usd?: number;
  max_turns_per_task?: number;
  on_exhausted?: 'drain' | 'stop';
}

export interface ReviewSection {
  budget?: Budget;
}

/**
 * One intake trigger as factory.schema.json's `$defs/trigger` writes it in `spec.board.triggers`
 * (ADR 0026). Keys are the schema's camelCase keys, unlike the model above: the trigger is read and
 * written in the file, not through the section API. A trigger has one `on` event key, so "+ Add
 * another event" has nothing to add to yet.
 */
export interface Trigger {
  name: string;
  enabled?: boolean | undefined;
  /** The event key: github.check_run.failed, github.issues.labeled, schedule or webhook.<endpoint>[.<event>]. */
  on: string;
  /** Five cron fields; only when `on` is schedule. */
  cron?: string | undefined;
  timeZone?: string | undefined;
  filter?: TriggerFilter | undefined;
  only?: TriggerOnly | undefined;
  create: TriggerCreate;
  limits?: TriggerLimits | undefined;
}

export interface TriggerFilter {
  branch?: string | undefined;
  repo?: string | undefined;
  label?: string | undefined;
  paths?: string[] | undefined;
  authors?: string[] | undefined;
  skipBots?: boolean | undefined;
}

export interface TriggerOnly {
  failuresInARow?: number | undefined;
  /** A dotted event field, such as check.name. */
  dedupe?: string | undefined;
}

export interface TriggerCreate {
  type: string;
  station: string;
  lane?: string | undefined;
  expedite?: boolean | undefined;
  /** Text with {{field}} placeholders filled from the event. */
  title?: string | undefined;
  /** Text with {{field}} placeholders; the event's values reach agents as untrusted data only. */
  brief?: string | undefined;
}

export interface TriggerLimits {
  perDay?: number | undefined;
  pauseOnBudget?: boolean | undefined;
}

/** Each section's value, by section. */
export interface SectionValues {
  basics: BasicsSection;
  line: LineSection;
  agents: AgentsSection;
  connections: ConnectionsSection;
  review: ReviewSection;
}

export type Model = SectionValues;

/** The typed view of a model goald answered. */
export function modelOf(model: FactoryModel): Model {
  return model as unknown as Model;
}

/** A model as goald takes it back. */
export function factoryModelOf(model: Model): FactoryModel {
  return model as unknown as FactoryModel;
}

/** The section of a model pointer ("/line/stations/3" → line); undefined outside the model. */
export function sectionOfPointer(pointer: string): Section | undefined {
  const s = pointer.split('/')[1];
  return (sections as readonly string[]).includes(s ?? '') ? (s as Section) : undefined;
}
