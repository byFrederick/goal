// The editor's sections and its URL state (D §10.1): /definition/:section with its search
// params, shared by Definition and the wizard. Search params carry names, ids and line numbers
// only, never content (D §11).

/** The five sections with tiles, then Versions (reached from the header). */
export const editorSections = ['basics', 'line', 'agents', 'environments', 'knowledge', 'review', 'versions'] as const;

export type EditorSection = (typeof editorSections)[number];

export function isEditorSection(value: string): value is EditorSection {
  return (editorSections as readonly string[]).includes(value);
}

export interface SectionInfo {
  /** The tile's and the h2's name. */
  label: string;
  /** The sentence under the section's h2. */
  description: string;
}

/** The editor route a model section (goald's model still calls the Environments page's section connections) is shown on: the model's connections are the Environments page. */
export const routeSection = (section: EditorSection | 'connections'): EditorSection =>
  section === 'connections' ? 'environments' : section;

const environmentsInfo: SectionInfo = {
  label: 'Environments',
  description: 'What each agent’s box installs and what it can reach. Keys come from Secrets.',
};

export const sectionInfo: Record<EditorSection | 'connections', SectionInfo> = {
  basics: {
    label: 'Basics',
    description: 'What this factory is for and where its work lives. Agents see the purpose in every prompt.',
  },
  line: { label: 'Line', description: 'Stations in the order work moves through them.' },
  agents: {
    label: 'Agents',
    description:
      'Who works on the line. Each agent gets a role, a model, a prompt, skills and the built-in tools it may use.',
  },
  environments: environmentsInfo,
  knowledge: {
    label: 'Knowledge',
    description: 'The folders agents read, and the facts every run starts with.',
  },
  connections: environmentsInfo,
  review: { label: 'Review and publish', description: 'Checks, the changes from the running version, and publish.' },
  versions: { label: 'Versions', description: 'Every published version, the one that runs, compare and restore.' },
};

/** The URL state of a section (`/definition/:section?…`). */
export interface DefinitionSearch {
  /** The YAML view instead of Visual. */
  mode?: 'yaml';
  /** The YAML view's file (factory.yaml when absent). */
  file?: string;
  /** A line of the YAML view's file. */
  line?: number;
  /** The entity the section shows or focuses. */
  agent?: string;
  station?: string;
  connection?: string;
  /** Environments: the tab (environments/<name>.yaml); Add a tool: the environment it adds to. */
  environment?: string;
  /** Review's comparison, "<from>..<to>" (current..draft). */
  compare?: string;
  /** Agents: open the skill picker. */
  add?: 'skill';
  /** The draft request the Ask bar shows (its id, from Ask Jonah for a fix or an earlier ask). */
  request?: string;
}

const text = (v: unknown): string | undefined => (typeof v === 'string' && v !== '' ? v : undefined);

/** The search params a section route accepts; anything else is dropped. */
export function validateDefinitionSearch(search: Record<string, unknown>): DefinitionSearch {
  const line = Number(search.line);
  const out: DefinitionSearch = {};
  if (search.mode === 'yaml') out.mode = 'yaml';
  if (search.add === 'skill') out.add = 'skill';
  if (Number.isInteger(line) && line > 0) out.line = line;
  for (const key of ['file', 'agent', 'station', 'connection', 'environment', 'compare', 'request'] as const) {
    const v = text(search[key]);
    if (v !== undefined) out[key] = v;
  }
  return out;
}
