// Links and navigation that change only the section's search params (?agent=, ?connection=),
// keeping the route (Definition or the wizard) and its other params (?factory= in the wizard);
// useGoTo moves to a section (or stays) with exactly the search params given.
import { Link, useNavigate } from '@tanstack/react-router';
import { type ComponentProps, useCallback } from 'react';
import type { DefinitionSearch, EditorSection } from './sections';

/** Search params to set; undefined removes one. */
export type SearchPatch = { [K in keyof DefinitionSearch]?: DefinitionSearch[K] | undefined };

const patched = (patch: SearchPatch) => (prev: Record<string, unknown>) => {
  const next: Record<string, unknown> = { ...prev, ...patch };
  for (const [k, v] of Object.entries(next)) if (v === undefined) delete next[k];
  return next as never;
};

type SearchLinkProps = { patch: SearchPatch } & Pick<
  ComponentProps<'a'>,
  'className' | 'children' | 'aria-current' | 'aria-label' | 'onClick'
>;

export function SearchLink({ patch, ...props }: SearchLinkProps) {
  return <Link to="." search={patched(patch)} {...(props as object)} />;
}

/**
 * A link to another section of the same editor with its search params: Definition's
 * /definition/$section or the wizard's /new/$step (whose search keeps ?factory=).
 */
export function SectionLink({ section, patch, ...props }: SearchLinkProps & { section: EditorSection }) {
  const params = (prev: Record<string, unknown>) => ({ ...prev, section, step: section }) as never;
  return <Link to="." params={params} search={patched(patch)} {...(props as object)} />;
}

/** Moves to other search params of the same section, as a new history entry unless `replace`. */
export function useSetSearch(): (patch: SearchPatch, opts?: { replace?: boolean }) => void {
  const navigate = useNavigate();
  return useCallback(
    (patch, opts) => void navigate({ to: '.', search: patched(patch), replace: opts?.replace ?? false }),
    [navigate],
  );
}

/**
 * Goes to a section of the same editor (or stays, without one) with exactly `search`: Definition's
 * /definition/$section or the wizard's /new/$step, whose ?factory= is kept.
 */
export function useGoTo(): (section: EditorSection | undefined, search: DefinitionSearch, replace?: boolean) => void {
  const navigate = useNavigate();
  return useCallback(
    (section, search, replace = false) =>
      void navigate({
        to: '.',
        ...(section
          ? { params: (prev: Record<string, unknown>) => ({ ...prev, section, step: section }) as never }
          : {}),
        search: (prev: Record<string, unknown>) =>
          ({ ...(typeof prev.factory === 'string' ? { factory: prev.factory } : {}), ...search }) as never,
        replace,
      }),
    [navigate],
  );
}
