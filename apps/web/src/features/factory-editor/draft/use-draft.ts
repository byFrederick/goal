// The draft session in React: DraftSessionProvider (mounted by the Definition layout and the
// wizard, keyed by factory so a new factory gets a new session) creates one DraftSession, feeds
// it B's draft model from TanStack Query (the stream's `definition` events invalidate it; the
// session's own saves refetch it once per burst), or the running version while no draft is open,
// and provides it; useDraft reads a slice of its snapshot through useSyncExternalStore. Without
// definition:edit the session is read-only (`readOnly`, keyed by the caller so a role change
// makes a new one) and shows the running version even while someone edits a draft (canvas
// Define-View); a factory never published shows its draft.
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  createContext,
  createElement,
  type ReactNode,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react';
import { ApiError } from '../../../api/client';
import { defKeys, draftModelQuery, factoryQuery, versionModelQuery } from '../../../api/queries';
import type { Factory } from '../../../api/types';
import type { Model, Section, SectionValues } from '../../../shared/definition/model';
import { DraftSession, type SessionState } from './session';

export const DraftSessionContext = createContext<DraftSession | null>(null);

export interface DraftSessionProviderProps {
  org: string;
  /** The factory's id. */
  factory: string;
  /** The caller may not change the definition: show it only (key the provider by it). */
  readOnly?: boolean;
  children: ReactNode;
}

export function DraftSessionProvider({ org, factory, readOnly = false, children }: DraftSessionProviderProps) {
  const client = useQueryClient();
  const [session] = useState(
    () =>
      new DraftSession({
        org,
        factory,
        readOnly,
        refresh: () => void client.invalidateQueries({ queryKey: defKeys.draft(org, factory) }),
        // Before the publish or discard refetches anything: the draft's queries go (an
        // invalidation would read the closed draft through observers not yet rendered again)
        // and the factory has no draft, so the editor reads none until the next one opens.
        closed: () => {
          client.removeQueries({ queryKey: defKeys.draft(org, factory) });
          client.setQueryData<Factory>(defKeys.factory(org, factory), (f) => (f ? { ...f, draft: null } : f));
        },
      }),
  );
  useEffect(() => {
    session.activate();
    return () => session.dispose();
  }, [session]);

  const open = useDraftOpen(session);
  const draft = useQuery({ ...draftModelQuery(org, factory), enabled: open === true });
  const refusal = draft.error instanceof ApiError ? draft.error : undefined;
  const noDraft = open === false || refusal?.code === 'no-draft';
  const current = useQuery({ ...versionModelQuery(org, factory, 'current'), enabled: noDraft });

  useEffect(() => {
    if (draft.data) session.adopt(draft.data);
  }, [draft.data, session]);
  useEffect(() => {
    if (refusal?.code === 'no-draft') session.adoptMissing();
    else if (refusal?.code === 'yaml-invalid') {
      session.adoptParseError(
        refusal.problem.parse_error ?? { line: 0, column: 0, message: refusal.detail ?? refusal.title },
      );
    }
  }, [refusal, session]);
  useEffect(() => {
    if (noDraft && current.data) session.adoptVersion(current.data);
  }, [noDraft, current.data, session]);

  return createElement(DraftSessionContext.Provider, { value: session }, children);
}

/**
 * Whether the factory has a draft open: this session opened or adopted one, or the factory says
 * so (its `draft`); undefined until the factory has loaded. "No draft" is a state the factory
 * names, so the editor reads the draft only while one is open and a factory without one makes
 * no request that fails (a 404 `no-draft` is then a draft discarded elsewhere).
 */
export function useDraftOpen(session: DraftSession): boolean | undefined {
  const opened = useSyncExternalStore(session.subscribe, () => session.getSnapshot().hasDraft);
  const detail = useQuery(factoryQuery(session.org, session.factory)).data;
  if (opened) return true;
  if (!detail) return undefined;
  // Read-only, the running version is what there is to see; someone else's draft is theirs.
  if (session.readOnly && detail.current !== null) return false;
  return detail.draft !== null;
}

/** The editor only shows the definition: the caller lacks definition:edit (canvas Define-View). */
export function useViewOnly(): boolean {
  return useDraftSession().readOnly;
}

/** The editor's draft session; only inside DraftSessionProvider. */
export function useDraftSession(): DraftSession {
  const session = useContext(DraftSessionContext);
  if (!session) throw new Error('useDraftSession outside DraftSessionProvider');
  return session;
}

/**
 * A slice of the session's snapshot; the component renders again only when the slice changes
 * identity (select a field or derive a primitive; a selector building a new object each time
 * renders on every change).
 */
export function useDraft<T>(select: (state: SessionState) => T): T {
  const session = useDraftSession();
  const cache = useRef<{ snap?: SessionState; select?: (state: SessionState) => T; value?: T }>({});
  const read = () => {
    const snap = session.getSnapshot();
    const c = cache.current;
    if (c.snap !== snap || c.select !== select) cache.current = { snap, select, value: select(snap) };
    return cache.current.value as T;
  };
  return useSyncExternalStore(session.subscribe, read, read);
}

/** One section's value as the editor shows it (server plus unsaved edits). */
export function useSection<S extends Section>(section: S): SectionValues[S] {
  return useDraft((s) => s.sections[section].local) as SectionValues[S];
}

/** The whole model as the editor shows it: what edits touching several sections start from. */
export function useModel(): Model {
  const sections = useDraft((s) => s.sections);
  return useMemo(
    () => ({
      basics: sections.basics.local,
      line: sections.line.local,
      agents: sections.agents.local,
      connections: sections.connections.local,
      review: sections.review.local,
    }),
    [sections],
  );
}
