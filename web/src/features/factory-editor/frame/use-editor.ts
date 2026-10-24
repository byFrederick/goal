// What every section reads beside its own value (D §10.4): B's validation of the draft at the
// session's revision, the problems and the edited fields at a model pointer.
import { useQuery } from '@tanstack/react-query';
import { useMemo } from 'react';
import { validationQuery } from '../../../api/queries';
import type { DefinitionProblem, Validation } from '../../../api/types';
import { useDraft, useDraftSession } from '../draft/use-draft';

const none: DefinitionProblem[] = [];

/** B's validation of the draft at the session's revision; undefined without a draft or while it loads. */
export function useValidation(): Validation | undefined {
  const session = useDraftSession();
  const revision = useDraft((s) => (s.hasDraft ? s.revision : 0));
  const { data } = useQuery({ ...validationQuery(session.org, session.factory, revision), enabled: revision > 0 });
  return data;
}

const under = (pointer: string) => (p: string | undefined) =>
  p !== undefined && (p === pointer || p.startsWith(`${pointer}/`));

/** The problems at a model pointer or inside it ("/basics/name", "/line/stations/3"). */
export function useProblems(pointer: string): DefinitionProblem[] {
  const problems = useValidation()?.problems ?? none;
  return useMemo(() => problems.filter((p) => under(pointer)(p.pointer)), [problems, pointer]);
}

/** The first problem's message at a pointer, for a field's error line. */
export function useProblemText(pointer: string): string | undefined {
  return useProblems(pointer)[0]?.message;
}

/** The pointers the draft changed at or inside `pointer` (B's `edited`): what "Edited · Undo" reverts. */
export function useEdited(pointer: string): string[] {
  const edited = useDraft((s) => s.edited);
  return useMemo(() => Object.keys(edited).filter(under(pointer)), [edited, pointer]);
}
