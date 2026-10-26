// The text of each factory rule the session has: what is typed in the draft, else the file goald
// holds (read once through the session, so a render does not refetch it). Shared by the Rules area
// and each agent's effective rules.
import { useEffect, useState } from 'react';
import { ruleFile } from '../draft/ops';
import { useDraft, useDraftSession } from '../draft/use-draft';

/** Each rule's text by name: undefined while it loads, null when its file is missing or unreadable. */
export function useRuleTexts(names: readonly string[]): Record<string, string | null | undefined> {
  const session = useDraftSession();
  const files = useDraft((s) => s.files);
  const [loaded, setLoaded] = useState<Record<string, string | null>>({});
  const key = names.join('\n');

  useEffect(() => {
    let live = true;
    for (const name of key === '' ? [] : key.split('\n')) {
      session.fileText(ruleFile(name)).then(
        (text) => live && setLoaded((m) => ({ ...m, [name]: text })),
        () => live && setLoaded((m) => ({ ...m, [name]: null })),
      );
    }
    return () => {
      live = false;
    };
  }, [key, session]);

  return Object.fromEntries(names.map((name) => [name, files.get(ruleFile(name))?.text ?? loaded[name]]));
}

/** One rule's text, as useRuleTexts gives it. */
export function useRuleText(name: string): string | null | undefined {
  return useRuleTexts([name])[name];
}
