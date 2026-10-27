// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getDraftFile, openDraft, publishDraft, putDraftFile } from '../../api/definitions';
import { getIntake } from '../../api/intake';
import type { Factory, Intake, OrgSummary } from '../../api/types';
import { renderFactoryPage } from '../../test/route';
import { IntakePage } from './intake-page';
import { draftOpenNotice } from './trigger-toggle';

vi.mock('../../api/intake', () => ({ getIntake: vi.fn() }));
vi.mock('../../api/definitions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/definitions')>()),
  openDraft: vi.fn(),
  getDraftFile: vi.fn(),
  putDraftFile: vi.fn(),
  publishDraft: vi.fn(),
}));

afterEach(cleanup);

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };

function factoryWith(over: Partial<Factory> & { can?: string[] } = {}): Factory {
  return {
    id: 'factory-1',
    org_id: 'org-1',
    name: 'widgets',
    title: 'Widgets',
    work_types: [],
    stations: [],
    current: {
      id: 'version-3',
      number: 3,
      published_at: '2026-10-20T10:00:00Z',
      published_by: { kind: 'user', id: 'u', name: 'Freddy' },
    },
    draft: null,
    paused: false,
    created_at: '2026-10-01T00:00:00Z',
    role: 'owner',
    can: ['definition:edit'],
    ...over,
  } as unknown as Factory;
}

const yaml = `# The factory. Keep this comment.
spec:
  board:
    triggers:
      - name: flaky-test-on-main
        on: github.check_run.failed
        create: { type: fix, station: build }
      - name: dependency-audit
        enabled: true
        on: schedule
        create: { type: infra, station: spec }
`;

const intake: Intake = {
  items: 3,
  from: '2026-10-20',
  to: '2026-10-22',
  sources: { github: 3, people: 0, cli: 0, api: 0, schedule: 0, webhook: 0 },
  triggers: [
    {
      name: 'flaky-test-on-main',
      enabled: true,
      source: 'github',
      on: 'github.check_run.failed',
      when: 'github.check_run.failed',
      create: { type: 'fix', station: 'build', lane: null, expedite: false },
      days: [0, 0, 0, 0, 0, 0, 3],
      problem: null,
    },
    {
      name: 'dependency-audit',
      enabled: true,
      source: 'schedule',
      on: 'schedule',
      when: '0 9 * * MON, UTC',
      create: { type: 'infra', station: 'spec', lane: null, expedite: false },
      days: [0, 0, 0, 0, 0, 0, 0],
      problem: null,
    },
  ],
};

function renderSwitches(factory: Factory) {
  return renderFactoryPage({
    page: () => <IntakePage group="software" onGroupChange={() => {}} />,
    path: 'intake/',
    org,
    factory,
  });
}

beforeEach(() => {
  vi.mocked(getIntake).mockResolvedValue(intake);
  vi.mocked(openDraft).mockResolvedValue({ id: 'draft-1', revision: 1 } as never);
  vi.mocked(getDraftFile).mockResolvedValue({ text: yaml, sha256: 'sha-1' });
  vi.mocked(putDraftFile).mockResolvedValue({ revision: 7, updated_at: '2026-10-27T10:00:00Z' } as never);
  vi.mocked(publishDraft).mockResolvedValue({} as never);
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('the Intake switch', () => {
  it("edits only that trigger's enabled in factory.yaml and publishes the draft against the version shown", async () => {
    renderSwitches(factoryWith());
    const toggle = await screen.findByRole('switch', { name: 'flaky-test-on-main' });
    fireEvent.click(toggle);

    await waitFor(() => expect(publishDraft).toHaveBeenCalledTimes(1));
    expect(openDraft).toHaveBeenCalledWith('org-1', 'factory-1');
    const [, , file, text, base] = vi.mocked(putDraftFile).mock.calls[0] ?? [];
    expect(file).toBe('factory.yaml');
    expect(base).toEqual({ sha256: 'sha-1' });
    const lines = (value: string) => value.split('\n');
    expect(lines(text ?? '').filter((line) => !lines(yaml).includes(line))).toEqual(['        enabled: false']);
    expect(lines(yaml).filter((line) => !lines(text ?? '').includes(line))).toEqual([]);
    expect(text).toContain('# The factory. Keep this comment.');
    expect(publishDraft).toHaveBeenCalledWith('org-1', 'factory-1', {
      revision: 7,
      expected_current_version_id: 'version-3',
      message: 'Turn off the trigger flaky-test-on-main',
    });
  });

  it('reverts the switch and says why when the publish fails', async () => {
    vi.mocked(publishDraft).mockRejectedValue(new Error('The definition changed. Read it again.'));
    renderSwitches(factoryWith());
    const toggle = await screen.findByRole('switch', { name: 'flaky-test-on-main' });
    fireEvent.click(toggle);

    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.getByText('Not changed. The definition changed. Read it again.')).toBeTruthy();
    expect(toggle.getAttribute('aria-checked')).toBe('true');
  });

  it('changes nothing while a draft is open and says to publish or discard it in Definition', async () => {
    renderSwitches(factoryWith({ draft: { id: 'draft-1', revision: 2 } as never }));
    const toggle = await screen.findByRole('switch', { name: 'flaky-test-on-main' });
    fireEvent.click(toggle);

    expect(await screen.findByText(draftOpenNotice)).toBeTruthy();
    expect(openDraft).not.toHaveBeenCalled();
    expect(publishDraft).not.toHaveBeenCalled();
    expect(toggle.getAttribute('aria-checked')).toBe('true');
  });

  it('is disabled for people who cannot edit the definition', async () => {
    renderSwitches(factoryWith({ can: ['item:operate'] } as never));
    const toggle = await screen.findByRole('switch', { name: 'flaky-test-on-main' });

    expect(toggle.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(toggle);
    expect(openDraft).not.toHaveBeenCalled();
    expect(within(screen.getByRole('region', { name: /Triggers/ })).getAllByRole('switch')).toHaveLength(2);
  });
});
