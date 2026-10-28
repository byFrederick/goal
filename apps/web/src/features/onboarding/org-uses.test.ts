// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { getOrgUses, setOrgUses } from './org-uses';

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  localStorage.clear();
});

describe('the uses a person picked on step 1', () => {
  it('are kept per organization in this browser', () => {
    setOrgUses('org-acme', ['build', 'support']);
    setOrgUses('org-goal', ['operations']);

    expect(getOrgUses('org-acme')).toEqual(['build', 'support']);
    expect(getOrgUses('org-goal')).toEqual(['operations']);
    expect(getOrgUses('org-none')).toEqual([]);
  });

  it('drop a saved value that is not a use', () => {
    localStorage.setItem('goal.org-uses.org-acme', JSON.stringify(['build', 'space-travel', 3]));

    expect(getOrgUses('org-acme')).toEqual(['build']);
  });
});
