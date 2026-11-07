import { describe, expect, it } from 'vitest';
import { blockedWords } from './blocked-words';

describe('blockedWords', () => {
  it('says a missing model key in plain words and links to the factory settings', () => {
    const w = blockedWords('model_key_missing');
    expect(w?.sentence).toBe('Agents need a model key: add one in Settings');
    expect(w?.fix).toEqual({ label: 'Settings', to: '/o/$org/f/$factory/settings', hash: 'runtime' });
    expect(w?.sentence).not.toContain('{{');
    expect(w?.sentence).not.toContain('runtime.modelKey');
  });

  it('gives nothing for no code or an unknown one', () => {
    expect(blockedWords(null)).toBeUndefined();
    expect(blockedWords(undefined)).toBeUndefined();
    expect(blockedWords('something_else')).toBeUndefined();
    expect(blockedWords('toString')).toBeUndefined();
  });
});
