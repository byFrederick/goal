import { describe, expect, it } from 'vitest';
import { validateWizardSearch } from './wizard-search';

describe('validateWizardSearch', () => {
  it('keeps a model preset the Line step saved', () => {
    expect(validateWizardSearch({ factory: 'f1', preset: 'quality' }).preset).toBe('quality');
    expect(validateWizardSearch({ factory: 'f1', preset: 'cost' }).preset).toBe('cost');
  });

  it('drops a preset it does not know', () => {
    expect(validateWizardSearch({ factory: 'f1', preset: 'fast' })).not.toHaveProperty('preset');
    expect(validateWizardSearch({ factory: 'f1', preset: ['cost'] })).not.toHaveProperty('preset');
  });
});
