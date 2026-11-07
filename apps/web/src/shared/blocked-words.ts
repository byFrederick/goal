// Why an item is blocked, in plain words: one sentence and a link to the fix for each block code goald
// gives (the blocked event's data.code, InboxBlocked.code). It branches on the code only, never on the
// reason's text. A missing or unknown code gives nothing, so callers keep the reason as they show it today.

/** Where a person fixes a block: the route and the settings group it opens. */
export interface BlockedFix {
  /** The link's words; the sentence ends with them. */
  label: string;
  to: '/o/$org/f/$factory/settings';
  hash: string;
}

export interface BlockedWords {
  /** The whole sentence, ending with the fix link's label. */
  sentence: string;
  fix: BlockedFix;
}

const words: Record<string, BlockedWords> = {
  model_key_missing: {
    sentence: 'Agents need a model key: add one in Settings',
    fix: { label: 'Settings', to: '/o/$org/f/$factory/settings', hash: 'runtime' },
  },
};

export function blockedWords(code: string | null | undefined): BlockedWords | undefined {
  return code && Object.hasOwn(words, code) ? words[code] : undefined;
}
