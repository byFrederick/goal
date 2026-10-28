// The wizard's URL state (D §10.1): the section's search params plus the new factory's id and,
// on Start, the way to start and the template. Kept apart from the wizard's components so the
// route's validateSearch does not pull them into the initial bundle.
import { type DefinitionSearch, validateDefinitionSearch } from './sections';
import type { PresetId } from './start/model-presets';
import type { StartKind } from './start/start-choice';

export interface WizardSearch extends DefinitionSearch {
  /** The new factory's id, from step 2 on. */
  factory?: string;
  /** How to start, from the Factories home's cards. */
  from?: StartKind;
  /** The template chosen on Start. */
  template?: string;
  /** The model preset last saved on the Line step. */
  preset?: PresetId;
}

export function validateWizardSearch(search: Record<string, unknown>): WizardSearch {
  const out: WizardSearch = validateDefinitionSearch(search);
  if (typeof search.factory === 'string' && search.factory !== '') out.factory = search.factory;
  if (search.from === 'blank' || search.from === 'template' || search.from === 'clone' || search.from === 'yaml') {
    out.from = search.from;
  }
  if (typeof search.template === 'string' && search.template !== '') out.template = search.template;
  if (search.preset === 'quality' || search.preset === 'cost') out.preset = search.preset;
  return out;
}
