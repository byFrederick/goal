// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { keys } from '../../../api/queries';
import type { ToolTemplate } from '../../../api/types';
import { readEnv } from '../environments/env-file';
import { CatalogList } from './catalog-list';
import { CustomPanel } from './custom-panel';
import { TemplatePanel } from './template-panel';

const calls = vi.hoisted(() => ({
  createSecret: vi.fn(),
  testEnvironment: vi.fn(),
  navigate: vi.fn(),
  editFile: vi.fn(),
  flush: vi.fn(() => Promise.resolve()),
}));

vi.mock('../../../api/secrets', () => ({ createSecret: calls.createSecret }));
vi.mock('../../../api/definitions', () => ({ testEnvironment: calls.testEnvironment }));
vi.mock('../../../shared/org', () => ({ useOrg: () => ({ id: 'o1', slug: 'acme', name: 'Acme' }) }));
vi.mock('../../../shared/factory', () => ({
  useFactoryParams: () => ({ org: 'acme', factory: 'goal' }),
  useFactory: () => ({ id: 'f1', name: 'goal' }),
}));
vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => calls.navigate,
  Link: ({
    children,
    to: _to,
    params: _p,
    search: _s,
    ...props
  }: {
    children?: ReactNode;
    to?: string;
    params?: unknown;
    search?: unknown;
  }) => (
    <a href="/x" {...props}>
      {children}
    </a>
  ),
}));
const envText = 'apiVersion: goal/v1\nkind: Environment\nspec:\n  # keep me\n  tools: [go@1.26]\n';
vi.mock('../draft/use-draft', () => ({
  useDraftSession: () => ({
    org: 'o1',
    factory: 'f1',
    fileText: () => Promise.resolve(envText),
    editFile: calls.editFile,
    flush: calls.flush,
    getSnapshot: () => ({ error: null, files: new Map() }),
  }),
  useDraft: () => new Map(),
  useViewOnly: () => false,
}));
vi.mock('../environments/use-environments', () => ({
  useEnvironmentNames: () => ({ names: ['default', 'deploy'], ready: true }),
  useEnvironmentAgents: () => ({ default: ['architect', 'reviewer'], deploy: ['infra-engineer'] }),
}));

// jsdom has no scrollIntoView, which the select's list calls.
Element.prototype.scrollIntoView = () => {};

const sentry: ToolTemplate = {
  name: 'sentry',
  title: 'Sentry',
  description: 'Errors and releases',
  secrets: [{ slot: 'token', title: 'Sentry auth token', description: '', suggest: 'sentry-token' }],
  adds: {
    tools: [],
    env: {},
    files: {},
    mcp: {
      sentry: {
        url: 'https://mcp.sentry.dev/mcp',
        headers: { Authorization: 'Bearer {{token}}' },
        tools: { 'update_*': 'ask', 'delete_*': 'off' },
      },
    },
  },
  test: null,
};
const kubectl: ToolTemplate = {
  name: 'kubectl',
  title: 'kubectl',
  description: 'A cluster’s kubeconfig',
  secrets: [],
  adds: { tools: ['kubectl@1.31'], env: {}, files: {}, mcp: {} },
  test: null,
};

function wrap(node: ReactNode) {
  const client = new QueryClient();
  client.setQueryData(keys.secrets('o1'), [{ name: 'linear-key' }]);
  return render(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}

beforeEach(() => {
  calls.createSecret.mockResolvedValue({ name: 'sentry-token' });
  calls.testEnvironment.mockResolvedValue({
    environment: 'default',
    version: 'draft',
    servers: [
      {
        server: 'sentry',
        tested: true,
        ok: true,
        tools: ['search_issues', 'get_issue_details', 'update_issue', 'delete_issue'],
      },
    ],
  });
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('the catalog', () => {
  it('groups the tools and marks what the environment has', () => {
    wrap(
      <CatalogList
        templates={[kubectl, sentry]}
        selected="sentry"
        environment="default"
        spec={readEnv('spec:\n  tools: [kubectl@1.31]\n')}
      />,
    );
    expect(screen.getByRole('region', { name: 'MCP servers' })).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Command-line tools' })).toBeTruthy();
    expect(screen.getByText('In default')).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Your own' })).toBeTruthy();
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'zzz' } });
    expect(screen.getByText(/Nothing matches/)).toBeTruthy();
  });
});

describe('a template', () => {
  it('shows the lines it adds and waits for a secret before it can be added', () => {
    wrap(<TemplatePanel template={sentry} environment="default" onEnvironment={() => undefined} />);
    expect(screen.getByRole('heading', { name: 'Adds to environments/default.yaml' })).toBeTruthy();
    const preview = screen.getByRole('region', { name: 'Adds to environments/default.yaml' });
    expect(preview.getAttribute('tabindex')).toBe('0');
    expect(screen.getByText('url: https://mcp.sentry.dev/mcp')).toBeTruthy();
    expect(screen.getByText('Authorization: Bearer {{sentry-token}}')).toBeTruthy();
    expect(screen.getByText('architect, reviewer')).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Add to draft' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('creates the pasted secret, tests with it and shows the answer and the tools', async () => {
    wrap(<TemplatePanel template={sentry} environment="default" onEnvironment={() => undefined} />);
    fireEvent.change(screen.getByPlaceholderText('Paste sentry auth token'), { target: { value: 'sntrys_value' } });
    fireEvent.click(screen.getByRole('button', { name: 'Test' }));
    expect(await screen.findByText(/Sentry answered/)).toBeTruthy();
    expect(calls.createSecret).toHaveBeenCalledWith('o1', {
      name: 'sentry-token',
      value: 'sntrys_value',
      description: '',
      scope: { all_factories: false, factories: ['f1'] },
    });
    expect(calls.testEnvironment).toHaveBeenCalledWith('o1', 'f1', 'default', {
      template: { name: 'sentry', secrets: { token: 'sentry-token' }, env: {} },
    });
    expect(screen.getByText('4 tools found.')).toBeTruthy();
    // The found tools are grouped, and a group's mode is chosen here.
    fireEvent.click(
      within(screen.getByRole('group', { name: 'Read tools of sentry' })).getByRole('radio', { name: 'Off' }),
    );
    expect(screen.getByText(/search_issues: off/)).toBeTruthy();
  });

  it('writes only the draft’s environment file on Add to draft, keeping its comments', async () => {
    wrap(<TemplatePanel template={sentry} environment="default" onEnvironment={() => undefined} />);
    fireEvent.change(screen.getByPlaceholderText('Paste sentry auth token'), { target: { value: 'sntrys_value' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add to draft' }));
    await waitFor(() => expect(calls.editFile).toHaveBeenCalled());
    const [path, text] = calls.editFile.mock.calls[0] as [string, string];
    expect(path).toBe('environments/default.yaml');
    expect(text).toContain('# keep me');
    expect(readEnv(text).mcp.sentry).toMatchObject({
      url: 'https://mcp.sentry.dev/mcp',
      headers: { Authorization: 'Bearer {{sentry-token}}' },
      tools: { 'update_*': 'ask', 'delete_*': 'off' },
    });
    expect(calls.flush).toHaveBeenCalled();
    expect(await screen.findByText('Added to draft')).toBeTruthy();
  });

  it('shows Add to draft as busy, not disabled, while it saves, and sends one request', async () => {
    calls.flush.mockReturnValue(new Promise(() => undefined));
    wrap(<TemplatePanel template={sentry} environment="default" onEnvironment={() => undefined} />);
    fireEvent.change(screen.getByPlaceholderText('Paste sentry auth token'), { target: { value: 'sntrys_value' } });
    const button = screen.getByRole('button', { name: 'Add to draft' }) as HTMLButtonElement;
    fireEvent.click(button);
    await waitFor(() => expect(button.getAttribute('aria-busy')).toBe('true'));
    expect(button.getAttribute('aria-disabled')).toBe('true');
    expect(button.disabled).toBe(false);
    fireEvent.click(button);
    expect(calls.editFile).toHaveBeenCalledTimes(1);
  });

  it('shows Test as busy, not disabled, while it runs, and sends one request', async () => {
    calls.testEnvironment.mockReturnValue(new Promise(() => undefined));
    wrap(<TemplatePanel template={sentry} environment="default" onEnvironment={() => undefined} />);
    fireEvent.change(screen.getByPlaceholderText('Paste sentry auth token'), { target: { value: 'sntrys_value' } });
    const button = screen.getByRole('button', { name: 'Test' }) as HTMLButtonElement;
    fireEvent.click(button);
    await waitFor(() => expect(button.getAttribute('aria-busy')).toBe('true'));
    expect(button.getAttribute('aria-disabled')).toBe('true');
    expect(button.disabled).toBe(false);
    fireEvent.click(button);
    expect(calls.createSecret).toHaveBeenCalledTimes(1);
  });

  it('takes an existing secret without creating one', async () => {
    wrap(<TemplatePanel template={sentry} environment="default" onEnvironment={() => undefined} />);
    fireEvent.click(screen.getByRole('combobox', { name: 'Secret' }));
    fireEvent.click(await screen.findByRole('option', { name: 'linear-key' }));
    expect(screen.getByText('Authorization: Bearer {{linear-key}}')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Add to draft' }));
    await waitFor(() => expect(calls.editFile).toHaveBeenCalled());
    expect(calls.createSecret).not.toHaveBeenCalled();
  });
});

describe('Custom', () => {
  it('adds a command-line tool with a variable', async () => {
    let type: 'mcp' | 'cli' | 'api' = 'cli';
    wrap(<CustomPanel environment="deploy" onEnvironment={() => undefined} type={type} onType={(t) => (type = t)} />);
    fireEvent.change(screen.getByLabelText('Install'), { target: { value: 'kubectl@1.31' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add a variable' }));
    fireEvent.change(screen.getByLabelText('Name 1'), { target: { value: 'KUBE_CONTEXT' } });
    fireEvent.change(screen.getByLabelText('Value 1'), { target: { value: 'staging' } });
    expect(screen.getByText('tools:')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Add to draft' }));
    await waitFor(() => expect(calls.editFile).toHaveBeenCalled());
    const [path, text] = calls.editFile.mock.calls[0] as [string, string];
    expect(path).toBe('environments/deploy.yaml');
    expect(readEnv(text)).toMatchObject({ tools: ['go@1.26', 'kubectl@1.31'], env: { KUBE_CONTEXT: 'staging' } });
  });

  it('offers the three types and says why a server cannot be added yet', () => {
    wrap(<CustomPanel environment="default" onEnvironment={() => undefined} type="mcp" onType={() => undefined} />);
    expect(screen.getAllByRole('radio').map((r) => (r as HTMLInputElement).labels?.[0]?.textContent)).toEqual([
      'MCP server',
      'Command-line tool',
      'API',
    ]);
    expect(screen.getByText(/Name the server/)).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Add to draft' }) as HTMLButtonElement).disabled).toBe(true);
  });
});
