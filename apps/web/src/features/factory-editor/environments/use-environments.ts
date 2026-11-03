// The environment files of the draft, as the Environments section and Add a tool read and write them:
// the names (the draft's files, or the running version's while no draft is open, plus files this session
// has made), one file's text through the draft session (session.fileText, session.editFile: autosaved on
// its sha256, a 412 shows the editor's conflict dialog), and which agents each environment serves.
import { useQuery } from '@tanstack/react-query';
import { useEffect, useMemo } from 'react';
import { parse } from 'yaml';
import { draftFileQuery, draftFilesQuery, factoryQuery, versionFileQuery, versionQuery } from '../../../api/queries';
import { useDraft, useDraftSession, useViewOnly } from '../draft/use-draft';
import { defaultEnvironment, envName, envPath, readEnv } from './env-file';

/** The text of one file of the definition and whether it has loaded: adopted into the session, which keeps what is typed. */
export function useFileText(path: string): { text: string | undefined; ready: boolean } {
  const session = useDraftSession();
  const { org, factory: f } = session;
  const hasDraft = useDraft((s) => s.hasDraft);
  const files = useDefinitionFiles();
  const info = files.data?.find((x) => x.path === path);
  const state = useDraft((s) => s.files.get(path));
  const draftFile = useQuery({ ...draftFileQuery(org, f, path), enabled: hasDraft && info !== undefined });
  const versionFile = useQuery({
    ...versionFileQuery(org, f, 'current', path, info?.sha256 ?? ''),
    enabled: !hasDraft && info !== undefined,
  });
  const loaded = hasDraft ? draftFile.data : versionFile.data;
  const missing = state === undefined;
  // biome-ignore lint/correctness/useExhaustiveDependencies: `missing` re-adopts a file the session dropped (a publish)
  useEffect(() => {
    if (loaded) session.adoptFile(path, loaded);
  }, [loaded, path, session, missing]);
  const ready = state !== undefined || (files.data !== undefined && info === undefined);
  return { text: state?.text, ready };
}

/** The definition's files: the draft's, or the running version's while no draft is open. */
export function useDefinitionFiles() {
  const session = useDraftSession();
  const { org, factory: f } = session;
  const hasDraft = useDraft((s) => s.hasDraft);
  const current = useQuery(factoryQuery(org, f)).data?.current ?? null;
  const draft = useQuery({ ...draftFilesQuery(org, f), enabled: hasDraft });
  const version = useQuery({ ...versionQuery(org, f, 'current'), enabled: !hasDraft && current !== null });
  if (hasDraft) return { data: draft.data, error: draft.error, refetch: draft.refetch };
  return { data: current === null ? [] : version.data?.files, error: version.error, refetch: version.refetch };
}

/** The environments, by name with default first: files of the definition and files this session made. */
export function useEnvironmentNames(): { names: string[]; ready: boolean; error: unknown; refetch: () => unknown } {
  const files = useDefinitionFiles();
  const local = useDraft((s) => s.files);
  const names = useMemo(() => {
    const found = new Set<string>();
    for (const f of files.data ?? []) {
      const n = envName(f.path);
      if (n) found.add(n);
    }
    for (const path of local.keys()) {
      const n = envName(path);
      if (n) found.add(n);
    }
    return [...found].sort((a, b) =>
      a === defaultEnvironment ? -1 : b === defaultEnvironment ? 1 : a.localeCompare(b),
    );
  }, [files.data, local]);
  return { names, ready: files.data !== undefined, error: files.error, refetch: files.refetch };
}

/** One environment: its text, its parsed spec, and `change`, which writes the edited text to the draft. */
export function useEnvironment(name: string) {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const path = envPath(name);
  const { text, ready } = useFileText(path);
  const spec = useMemo(() => readEnv(text ?? ''), [text]);
  const change = (edit: (text: string) => string) => {
    if (viewOnly) return;
    // The session's text is the latest (typing included), so two quick edits both land.
    const current = session.getSnapshot().files.get(path)?.text ?? text ?? '';
    const next = edit(current);
    if (next !== current) session.editFile(path, next);
  };
  return { path, text, spec, ready, change };
}

/** The agents that get each environment, from factory.yaml's agents (one without `environment` gets default). */
export function useEnvironmentAgents(): Record<string, string[]> | undefined {
  const { text } = useFileText('factory.yaml');
  return useMemo(() => {
    if (text === undefined) return undefined;
    let agents: unknown;
    try {
      agents = (parse(text) as { spec?: { agents?: unknown } } | null)?.spec?.agents;
    } catch {
      return {};
    }
    const out: Record<string, string[]> = {};
    for (const a of Array.isArray(agents) ? agents : []) {
      const { name, environment } = (a ?? {}) as { name?: unknown; environment?: unknown };
      if (typeof name !== 'string') continue;
      const env = typeof environment === 'string' && environment !== '' ? environment : defaultEnvironment;
      out[env] = [...(out[env] ?? []), name];
    }
    return out;
  }, [text]);
}
