// A region shown but not editable (a viewer's Definition, a member's Organization settings): the
// form controls inside show their values and take no input. Text fields are readOnly (focusable,
// their value read and selectable), selects and switches disabled. It is how the screen looks,
// not the guard: the screens also leave out what would change something, and goald refuses it.
import { createContext, type ReactNode, use } from 'react';

const ReadOnlyContext = createContext(false);

export function ReadOnly({ when = true, children }: { when?: boolean; children: ReactNode }) {
  return <ReadOnlyContext value={when}>{children}</ReadOnlyContext>;
}

/** Whether the controls here only show their values. */
export function useReadOnly(): boolean {
  return use(ReadOnlyContext);
}
