// The Floor's "Add a … agent" (the what-if): opens the factory's draft, raises the station's agents by one
// in the line section and saves it on the section's etag. Nothing is published here: the person reviews
// the change in the Line editor and publishes it there, so the running version does not change.
import { getDraftModel, openDraft, saveDraftSection } from '../../api/definitions';
import { modelOf } from '../../shared/definition/model';

/** Adds one agent to `station` (its agents default to one) and saves the line section on its etag. */
export async function addStationAgent(org: string, factory: string, station: string): Promise<void> {
  await openDraft(org, factory);
  const draft = await getDraftModel(org, factory);
  const line = modelOf(draft.model).line;
  const stations = (line.stations ?? []).map((s) => (s.id === station ? { ...s, agents: (s.agents ?? 1) + 1 } : s));
  await saveDraftSection(org, factory, 'line', draft.etags.line, { value: { ...line, stations } });
}
