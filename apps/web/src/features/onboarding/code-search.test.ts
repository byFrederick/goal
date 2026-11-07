import { describe, expect, it } from 'vitest';
import { carriedSearch, onboardingSearch } from './code-search';

describe('onboardingSearch access', () => {
  it('keeps none, read and write', () => {
    for (const access of ['none', 'read', 'write']) expect(onboardingSearch({ access }).access).toBe(access);
  });

  it('drops anything else', () => {
    for (const access of ['admin', '', 3, ['read'], undefined])
      expect(onboardingSearch({ access }).access).toBeUndefined();
  });
});

describe('carriedSearch', () => {
  const search = {
    url: 'https://github.com/acme/app',
    secret: 'github-token',
    template: 'starter',
    factory: '11111111-1111-4111-8111-111111111111',
    access: 'write',
  } as const;

  it('carries url, secret, template, factory and access', () => {
    expect(carriedSearch(search)).toEqual(search);
  });

  it('lets an override replace one field and keeps the rest', () => {
    expect(carriedSearch(search, { factory: undefined })).toEqual({ ...search, factory: undefined });
  });
});
