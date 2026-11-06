import { describe, expect, it } from 'vitest';
import { onboardingSearch } from './code-search';

describe('onboardingSearch access', () => {
  it('keeps none, read and write', () => {
    for (const access of ['none', 'read', 'write']) expect(onboardingSearch({ access }).access).toBe(access);
  });

  it('drops anything else', () => {
    for (const access of ['admin', '', 3, ['read'], undefined])
      expect(onboardingSearch({ access }).access).toBeUndefined();
  });
});
