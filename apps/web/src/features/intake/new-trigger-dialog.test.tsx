// @vitest-environment jsdom
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import { getVersionFile } from '../../api/definitions';
import { connectionsQuery, intakeKeys, keys } from '../../api/queries';
import type { Factory, OrgSummary } from '../../api/types';
import { connectionFixture } from '../../test/connection-fixture';
import { renderFactoryPage } from '../../test/route';
import { NewTriggerDialog } from './new-trigger-dialog';
import { setTriggerEnabled } from './set-trigger-enabled';

vi.mock('../../api/definitions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/definitions')>()),
  getVersionFile: vi.fn(),
}));

vi.mock('./set-trigger-enabled', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./set-trigger-enabled')>()),
  setTriggerEnabled: vi.fn().mockResolvedValue(undefined),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const definition = `spec:
  agents:
    - name: engineer
      tools: { allow: [Bash, Edit, 'Bash(sed -n*)', 'github__get_*'] }
    - name: scout
      environment: research
      builtins: { web: on }
      tools: { allow: [Bash] }
  board:
    triggers:
      - name: new-check
        on: schedule
        create: { type: fix, station: build }
`;

const environments = {
  default:
    'spec:\n  mcp:\n    github: { url: "https://example.com/github" }\n    cloudflare: { url: "https://example.com/cf" }\n',
  research:
    'spec:\n  mcp:\n    context7: { url: "https://example.com/c7" }\n    github: { url: "https://example.com/github" }\n',
};

/** An Intake answer; the suggestions are the definition's disabled triggers. */
const intakeFixture = (suggestions: unknown[]) => ({ triggers: [], suggestions });
const suggestion = (name: string, station: string) => ({
  name,
  source: 'schedule',
  on: 'schedule',
  when: '0 9 * * MON, UTC',
  create: { type: 'fix', station, lane: null, expedite: false, title: '' },
});

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: null };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'widgets',
  title: 'Widgets',
  work_types: [],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'owner',
  can: ['definition:edit'],
} as unknown as Factory;

const withDefinition = { ...factory, current: { id: 'v1', number: 1 } } as unknown as Factory;

function renderPicker(kinds: string[] = ['github', 'cloudflare'], suggestions: unknown[] = []) {
  const connections = kinds.map((kind) => connectionFixture({ name: `${kind}-acme`, kind }));
  return renderFactoryPage({
    page: () => <NewTriggerDialog />,
    path: 'intake/new',
    org,
    factory: withDefinition,
    seed: [
      [connectionsQuery(org.id).queryKey, connections],
      [intakeKeys.definition(org.id, factory.id, 'v1'), { text: definition, sha256: 'sha' }],
      [intakeKeys.environment(org.id, factory.id, 'v1', 'default'), { text: environments.default, sha256: 'a' }],
      [intakeKeys.environment(org.id, factory.id, 'v1', 'research'), { text: environments.research, sha256: 'b' }],
      [keys.intake(org.id, factory.id), intakeFixture(suggestions)],
    ],
  });
}

describe('New trigger picker', () => {
  it('lists Slack, Linear, Jira and Sentry as not available yet', async () => {
    renderPicker();
    expect(await screen.findByRole('dialog', { name: 'New trigger' })).toBeTruthy();
    expect(screen.getByText('Step 1 of 2: what starts it')).toBeTruthy();

    for (const name of ['Slack', 'Linear', 'Jira', 'Sentry']) {
      const source = screen.getByRole('button', { name });
      expect(source.getAttribute('aria-disabled')).toBe('true');
      expect(source.getAttribute('aria-pressed')).toBe('false');
    }
    fireEvent.focus(screen.getByRole('button', { name: 'Slack' }));
    expect((await screen.findAllByText('Not available yet')).length).toBeGreaterThan(0);
  });

  it("groups the sources by the organization's own connections", async () => {
    renderPicker([]);
    await screen.findByRole('dialog', { name: 'New trigger' });

    const group = (title: string) => screen.getByRole('heading', { name: title }).parentElement as HTMLElement;
    expect(within(group('Built in')).getByRole('button', { name: 'Schedule' })).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Connected' })).toBeNull();
    const notConnected = group('Not connected');
    expect(within(notConnected).getByRole('button', { name: 'GitHub' })).toBeTruthy();
    expect(within(notConnected).getByRole('link', { name: 'Connect GitHub' })).toBeTruthy();
    // A source that is not built has no Connect: connecting it would do nothing.
    expect(within(notConnected).getByRole('button', { name: 'Slack' })).toBeTruthy();
    expect(within(notConnected).queryByRole('link', { name: 'Connect Slack' })).toBeNull();
  });

  it('puts a source under Connected only when its connection exists and it is built', async () => {
    renderPicker(['github', 'slack']);
    await screen.findByRole('dialog', { name: 'New trigger' });

    const connected = screen.getByRole('heading', { name: 'Connected' }).parentElement as HTMLElement;
    expect(within(connected).getByRole('button', { name: 'GitHub' })).toBeTruthy();
    expect(within(connected).queryByRole('button', { name: 'Slack' })).toBeNull();
    const notConnected = screen.getByRole('heading', { name: 'Not connected' }).parentElement as HTMLElement;
    expect(within(notConnected).getByRole('button', { name: 'Cloudflare' })).toBeTruthy();
    expect(within(notConnected).getByRole('button', { name: 'Slack' })).toBeTruthy();
  });

  it('lists the built-in and connected sources as choosable', async () => {
    renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });

    for (const name of ['Schedule', 'Webhook', 'GitHub', 'Cloudflare']) {
      expect(screen.getByRole('button', { name }).getAttribute('aria-disabled')).toBeNull();
    }
  });

  it('lets only Issue labeled and Check run failed be chosen among the GitHub events', async () => {
    renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });

    expect((screen.getByRole('radio', { name: 'Check run failed' }) as HTMLInputElement).checked).toBe(true);
    const opened = screen.getByRole('radio', { name: 'Issue opened' }) as HTMLInputElement;
    expect(opened.getAttribute('aria-disabled')).toBe('true');
    fireEvent.click(opened);
    expect(opened.checked).toBe(false);

    fireEvent.click(screen.getByRole('radio', { name: 'Issue labeled' }));
    expect((screen.getByRole('radio', { name: 'Issue labeled' }) as HTMLInputElement).checked).toBe(true);
    expect(screen.getByRole('radio', { name: 'Push to a branch' }).getAttribute('aria-disabled')).toBe('true');
  });

  it('continues to a blank trigger named for the choice, with the chosen event in the address', async () => {
    const { router } = renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });
    expect(screen.getByText('Next: filters, what it creates and its limits, as a draft')).toBeTruthy();

    fireEvent.click(screen.getByRole('radio', { name: 'Issue labeled' }));
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/f/widgets/intake/new-issues-labeled'));
    expect(new URLSearchParams(router.state.location.search).get('on')).toBe('github.issues.labeled');
  });

  it('continues a schedule to a blank trigger with the schedule event', async () => {
    const { router } = renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });

    fireEvent.click(screen.getByRole('button', { name: 'Schedule' }));
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/f/widgets/intake/new-schedule'));
    expect(new URLSearchParams(router.state.location.search).get('on')).toBe('schedule');
  });

  it('lists Check with an agent after Schedule and continues with a name the definition lacks', async () => {
    const { router } = renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });

    const builtIn = screen.getByRole('heading', { name: 'Built in' }).parentElement as HTMLElement;
    const names = within(builtIn)
      .getAllByRole('button')
      .map((b) => b.textContent);
    expect(names.slice(0, 2)).toEqual(['Schedule', 'Check with an agent']);

    fireEvent.click(screen.getByRole('button', { name: 'Check with an agent' }));
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    // new-check is taken in the definition, so the next free name is used; the first agent checks.
    await waitFor(() => expect(router.state.location.pathname).toBe('/o/acme/f/widgets/intake/new-check-2'));
    const query = new URLSearchParams(router.state.location.search);
    expect(query.get('on')).toBe('schedule');
    expect(query.get('check')).toBe('engineer');
  });

  it("lists the MCP servers the agents' environments give them, never tools, and a server's check goes to the first agent that has it", async () => {
    const { router } = renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });

    const group = screen.getAllByRole('heading', { name: 'Check with an agent' })[0]?.parentElement as HTMLElement;
    await waitFor(() =>
      expect(
        within(group)
          .getAllByRole('button')
          .map((b) => b.textContent),
      ).toEqual(['GitHubits tool', 'Cloudflareits tool', 'context7its tool']),
    );

    fireEvent.click(within(group).getByRole('button', { name: /^context7/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    await waitFor(() => expect(router.state.location.href).toContain('check=scout'));
  });

  it('has no Continue for a source without an event key', async () => {
    renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });

    fireEvent.click(screen.getByRole('button', { name: 'Webhook' }));
    expect(screen.queryByRole('button', { name: 'Continue' })).toBeNull();
  });

  it("lists the API's suggestions and turns one on", async () => {
    renderPicker([], [suggestion('weekly-vulnerability-sweep', 'spec'), suggestion('nightly-review', 'review')]);
    await screen.findByRole('dialog', { name: 'New trigger' });

    expect(screen.getByRole('heading', { name: "Or turn on a suggestion from Widgets's definition" })).toBeTruthy();
    const row = screen.getByText('Weekly vulnerability sweep').parentElement as HTMLElement;
    expect(within(row).getByText('spec')).toBeTruthy();
    expect(screen.getByText('Nightly review')).toBeTruthy();

    fireEvent.click(within(row).getByRole('button', { name: 'Turn on' }));
    await waitFor(() =>
      expect(setTriggerEnabled).toHaveBeenCalledWith(
        expect.objectContaining({ trigger: 'weekly-vulnerability-sweep', enabled: true }),
      ),
    );
  });

  it('marks a suggestion button busy, not disabled, while its publish is pending', async () => {
    vi.mocked(setTriggerEnabled).mockReturnValueOnce(new Promise(() => {}));
    renderPicker([], [suggestion('nightly-review', 'review')]);
    await screen.findByRole('dialog', { name: 'New trigger' });
    const row = screen.getByText('Nightly review').parentElement as HTMLElement;
    const turnOn = within(row).getByRole('button', { name: 'Turn on' });
    fireEvent.click(turnOn);
    await waitFor(() => expect(turnOn.getAttribute('aria-busy')).toBe('true'));
    expect(turnOn.getAttribute('aria-disabled')).toBe('true');
    expect(turnOn.hasAttribute('disabled')).toBe(false);
  });

  it('shows no suggestions block when the API has none', async () => {
    renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });
    expect(screen.queryByRole('heading', { name: /turn on a suggestion/ })).toBeNull();
  });

  it('groups the sources, links Connect for those not connected and filters them by search', async () => {
    renderPicker(['github']);
    await screen.findByRole('dialog', { name: 'New trigger' });

    for (const title of ['Built in', 'Connected', 'Not connected']) {
      expect(screen.getByRole('heading', { level: 3, name: title })).toBeTruthy();
    }
    expect(screen.getByRole('link', { name: 'Connect Cloudflare' }).getAttribute('href')).toBe('/o/acme/access');
    expect(screen.getByRole('list', { name: 'New trigger steps' })).toBeTruthy();

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search sources' }), { target: { value: 'jir' } });
    expect(screen.getByRole('button', { name: 'Jira' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'GitHub' })).toBeNull();
    expect(screen.queryByRole('heading', { level: 3, name: 'Built in' })).toBeNull();
  });
});

describe('New trigger when an environment file cannot be read', () => {
  it('says so with Retry, keeps the servers that loaded, and refetches on Retry', async () => {
    const read = vi.mocked(getVersionFile);
    read.mockRejectedValueOnce(new ApiError({ title: 'Internal Server Error', status: 500 }));
    read.mockResolvedValue({ text: environments.research, sha256: 'b' });
    renderFactoryPage({
      page: () => <NewTriggerDialog />,
      path: 'intake/new',
      org,
      factory: withDefinition,
      seed: [
        [connectionsQuery(org.id).queryKey, [connectionFixture({ name: 'github-acme', kind: 'github' })]],
        [intakeKeys.definition(org.id, factory.id, 'v1'), { text: definition, sha256: 'sha' }],
        [intakeKeys.environment(org.id, factory.id, 'v1', 'default'), { text: environments.default, sha256: 'a' }],
        [keys.intake(org.id, factory.id), intakeFixture([])],
      ],
    });
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain("Couldn't read the agents' environments.");
    expect(within(alert.parentElement as HTMLElement).getByRole('button', { name: /^GitHub/ })).toBeTruthy();

    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull());
    expect(read).toHaveBeenCalledTimes(2);
  });
});

describe('New trigger phone tap targets', () => {
  it('makes each source button at least 44 px below md and 40 px from md', async () => {
    renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });
    for (const name of ['Schedule', 'Webhook', 'GitHub']) {
      const source = screen.getByRole('button', { name });
      expect(source.classList.contains('h-11')).toBe(true);
      expect(source.classList.contains('md:h-10')).toBe(true);
      expect(source.classList.contains('shrink-0')).toBe(true);
      expect(source.classList.contains('flex-1')).toBe(false);
    }
  });

  it('makes Close 44 px below md and 40 px from md', async () => {
    renderPicker();
    const dialog = await screen.findByRole('dialog', { name: 'New trigger' });
    const close = within(dialog).getByRole('button', { name: 'Close' });
    expect(close.classList.contains('max-md:size-11')).toBe(true);
    expect(close.classList.contains('size-10')).toBe(true);
  });

  it('makes each GitHub event row at least 44 px below md and 40 px from md', async () => {
    renderPicker();
    await screen.findByRole('dialog', { name: 'New trigger' });
    for (const name of ['Check run failed', 'Issue opened', 'Issue labeled']) {
      const row = screen.getByRole('radio', { name }).closest('div') as HTMLElement;
      expect(row.classList.contains('min-h-11')).toBe(true);
      expect(row.classList.contains('md:min-h-10')).toBe(true);
    }
  });
});
