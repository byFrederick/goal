// A form's edits over what goald last said. A field nobody touched shows the latest saved value,
// so a background change (a sync's end, a replaced secret, another tab's save) never throws away
// what someone is typing; the form compares `value` with the saved one to know it is dirty.
import { useState } from 'react';

export function useEdits<T extends object>(saved: T) {
  const [edits, setEdits] = useState<Partial<T>>({});
  return {
    value: { ...saved, ...edits } as T,
    set: <K extends keyof T>(key: K, v: T[K]) => setEdits((e) => ({ ...e, [key]: v })),
    /** After a save (the saved value is now what was typed) or to discard the edits. */
    reset: () => setEdits({}),
  };
}
