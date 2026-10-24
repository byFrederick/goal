import { describe, expect, test, vi } from 'vitest';
import { definitionInvalidations } from '../shared/invalidation';
import { discardDraft, getDraft, openDraft, saveDraftSection } from './definitions';
import { defKeys } from './queries';
import type { DefinitionEvent } from './types';

// goald's draft routes for one factory, behind the real client: each open starts a draft at
// revision 1 with a new id, each save adds one revision, a discard closes the draft.
const goald = vi.hoisted(() => {
  const state = { drafts: 0, id: '', revision: 0 };
  const json = (body: unknown, status = 200) =>
    new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
  const draft = () => ({ id: state.id, revision: state.revision, next_number: 9, change_count: 0, sections: [] });
  const fetch = async (request: Request) => {
    const path = new URL(request.url).pathname;
    if (request.method === 'POST' && path.endsWith('/draft')) {
      state.drafts += 1;
      state.id = `draft-${state.drafts}`;
      state.revision = 1;
      return json(draft(), 201);
    }
    if (request.method === 'GET' && path.endsWith('/draft')) return json(draft());
    if (request.method === 'DELETE' && path.endsWith('/draft')) return new Response(null, { status: 204 });
    state.revision += 1;
    return json({
      revision: state.revision,
      etag: `e${state.revision}`,
      updated_at: '',
      change_count: 1,
      problem_count: 0,
      sections: [],
    });
  };
  return { state, fetch };
});

vi.mock(import('./client'), async (original) => {
  const { createGoalClient } = await import('@goal/api-client');
  return { ...(await original()), goal: createGoalClient('http://goald.test', { fetch: goald.fetch }) };
});

const acme = 'acme';
const goal = '6f0c2a7e-3d1b-4c55-9a7e-1f2b3c4d5e60';

const save = () => saveDraftSection(acme, goal, 'basics', 'e', { value: {} });

/** The stream's event of a draft at revision. */
const draftEvent = (draft: string, revision: number): DefinitionEvent => ({
  org_id: acme,
  factory_id: goal,
  kind: 'draft',
  version_id: draft,
  revision,
});

describe("the stream's echo of this tab's draft saves", () => {
  test("re-reads nothing for this tab's own save", async () => {
    await openDraft(acme, goal);
    const { revision } = await save();

    expect(definitionInvalidations(acme, draftEvent(goald.state.id, revision))).toEqual([]);
  });

  test('re-reads the draft for a save elsewhere in a new draft after a discard, below the old revisions', async () => {
    await openDraft(acme, goal);
    for (let i = 0; i < 5; i++) await save();
    const old = await getDraft(acme, goal);
    await discardDraft(acme, goal, old.revision);

    // Another tab opens the next draft and saves it: revisions 1 and 2 of a new draft.
    goald.state.drafts += 1;
    const next = `draft-${goald.state.drafts}`;

    expect(definitionInvalidations(acme, draftEvent(next, 2))).toContainEqual(defKeys.draft(acme, goal));
  });

  test("still knows this tab's own saves of the new draft after a discard", async () => {
    const old = await getDraft(acme, goal);
    await discardDraft(acme, goal, old.revision);
    await openDraft(acme, goal);
    const { revision } = await save();

    expect(definitionInvalidations(acme, draftEvent(goald.state.id, revision))).toEqual([]);
    expect(definitionInvalidations(acme, draftEvent('draft-elsewhere', revision))).toContainEqual(
      defKeys.draft(acme, goal),
    );
  });
});
