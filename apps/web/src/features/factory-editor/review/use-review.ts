// What Review and Versions read beside the session: the draft, the version that runs, the next
// number, and B's comparison of two versions (refetched with each save of the draft).
import { keepPreviousData, useQueries, useQuery } from '@tanstack/react-query';
import { compareVersions, validateDefinition } from '../../../api/definitions';
import { defKeys, draftQuery, factoryQuery, versionFileQuery, versionQuery } from '../../../api/queries';
import type { Comparison, Draft, Factory, FileMap, Validation, VersionRef } from '../../../api/types';
import { useDraft, useDraftSession } from '../draft/use-draft';
import type { ComparePair } from './review';

export interface EditorFacts {
  org: string;
  /** The factory's id. */
  factory: string;
  detail: Factory | undefined;
  hasDraft: boolean;
  revision: number;
  draft: Draft | undefined;
  /** The version that runs; null before the first publish. */
  current: VersionRef | null;
  /** The number the next publish gives. */
  next: number;
}

export function useEditorFacts(): EditorFacts {
  const session = useDraftSession();
  const { org, factory } = session;
  const hasDraft = useDraft((s) => s.hasDraft);
  const revision = useDraft((s) => s.revision);
  const draft = useQuery({ ...draftQuery(org, factory), enabled: hasDraft }).data;
  const detail = useQuery(factoryQuery(org, factory)).data;
  const current = detail?.current ?? draft?.current ?? null;
  const next = hasDraft && draft ? draft.next_number : (current?.number ?? 0) + 1;
  return { org, factory, detail, hasDraft, revision, draft, current, next };
}

/** B's comparison; a side that is the draft is read again after each save. */
export function useComparison(pair: ComparePair | undefined): { data: Comparison | undefined; isError: boolean } {
  const { org, factory, revision } = useEditorFacts();
  const draftSide = pair?.from === 'draft' || pair?.to === 'draft';
  const query = useQuery({
    queryKey: defKeys.compare(org, factory, pair?.from ?? '', pair?.to ?? '', draftSide ? revision : 0),
    queryFn: ({ signal }) => compareVersions(org, factory, pair?.from, pair?.to, signal),
    enabled: pair !== undefined,
    placeholderData: keepPreviousData,
  });
  return { data: pair ? query.data : undefined, isError: query.isError };
}

/**
 * The checks of the version that runs: its files, which never change, sent to the validator. goald
 * keeps no checks per version, so this is the one way to read them without a draft.
 */
export function useCurrentChecks(enabled: boolean): { validation: Validation | undefined; isError: boolean } {
  const { org, factory, current } = useEditorFacts();
  const on = enabled && current !== null;
  const detail = useQuery({ ...versionQuery(org, factory, 'current'), enabled: on }).data;
  const files = useQueries({
    queries: (detail?.files ?? []).map((f) => ({
      ...versionFileQuery(org, factory, 'current', f.path, f.sha256),
      enabled: on,
    })),
  });
  const loaded = detail !== undefined && files.every((f) => f.data !== undefined);
  const map: FileMap = {};
  if (loaded) {
    for (const [i, f] of detail.files.entries()) map[f.path] = { text: files[i]?.data?.text ?? '' };
  }
  const query = useQuery({
    queryKey: [...defKeys.version(org, factory, detail?.id ?? ''), 'validation'],
    queryFn: () => validateDefinition(org, map),
    enabled: on && loaded,
    staleTime: Number.POSITIVE_INFINITY,
  });
  return { validation: query.data, isError: query.isError || files.some((f) => f.isError) };
}
