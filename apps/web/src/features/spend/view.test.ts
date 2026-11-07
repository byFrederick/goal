import { describe, expect, it } from 'vitest';
import { usedByText } from './view';

describe('usedByText', () => {
  it('names the factory when the organization has one', () => {
    expect(usedByText(1, ['Software'])).toBe('used by Software');
  });
  it('says "M of N factories" when some use the key', () => {
    expect(usedByText(2, ['Software', 'SRE', 'Support'])).toBe('used by 2 of 3 factories');
  });
  it('says "all N factories" when every factory uses the key', () => {
    expect(usedByText(3, ['Software', 'SRE', 'Support'])).toBe('used by all 3 factories');
  });
});
