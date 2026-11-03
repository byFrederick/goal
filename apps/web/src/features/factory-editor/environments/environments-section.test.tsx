// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import type { ReactNode } from 'react';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { keys } from '../../../api/queries';
import type { ServerTest } from '../../../api/types';
import { BoxCard, FilesCard, ToolsCard, VariablesCard } from './box-cards';
import { readEnv } from './env-file';
import { McpCard } from './mcp-card';

vi.mock('../../../shared/org', () => ({ useOrg: () => ({ id: 'o1', slug: 'acme', name: 'Acme' }) }));
vi.mock('../../../shared/factory', () => ({
  useFactoryParams: () => ({ org: 'acme', factory: 'goal' }),
  useFactory: () => ({ id: 'f1', name: 'goal' }),
}));
vi.mock('@tanstack/react-router', () => ({
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

const file = `apiVersion: goal/v1
kind: Environment
spec:
  # what the box installs
  tools: [go@1.26, gh@2]
  setup:
    - pnpm install --frozen-lockfile
  env:
    GH_TOKEN: "{{github-token}}"
    SENTRY_ORG: byfrederick
  mcp:
    linear:
      url: https://mcp.linear.app/mcp
      headers: { Authorization: "Bearer {{linear-key}}" }
      tools: { "delete_*": off }
`;

const linear: ServerTest = {
  server: 'linear',
  tested: true,
  ok: true,
  tools: ['list_issues', 'get_issue', 'create_comment', 'delete_issue'],
};

/** The cards over one file's text, which each edit rewrites, as the section does through the draft session. */
function Harness({ results = {}, readOnly = false }: { results?: Record<string, ServerTest>; readOnly?: boolean }) {
  const [text, setText] = useState(file);
  const spec = readEnv(text);
  const change = (edit: (t: string) => string) => setText((t) => edit(t));
  const props = { spec, change, readOnly, newSecret: () => undefined };
  return (
    <>
      <BoxCard {...props} />
      <ToolsCard {...props} />
      <VariablesCard {...props} checks={null} />
      <FilesCard {...props} />
      <McpCard
        environment="default"
        spec={spec}
        change={change}
        readOnly={readOnly}
        results={results}
        testing={undefined}
        testServer={() => undefined}
      />
      <pre data-testid="file">{text}</pre>
    </>
  );
}

function renderHarness(props: Parameters<typeof Harness>[0] = {}) {
  const client = new QueryClient();
  client.setQueryData(keys.secrets('o1'), [{ name: 'github-token' }, { name: 'linear-key' }]);
  return render(
    <QueryClientProvider client={client}>
      <Harness {...props} />
    </QueryClientProvider>,
  );
}

const fileText = () => screen.getByTestId('file').textContent ?? '';

afterEach(cleanup);

describe('the Environments cards', () => {
  it('show the box, the tools, the variables and the servers of the file', () => {
    renderHarness();
    expect(screen.getByText('go@1.26')).toBeTruthy();
    expect((screen.getByLabelText('Setup command 1') as HTMLInputElement).value).toBe('pnpm install --frozen-lockfile');
    expect((screen.getByLabelText('Value of SENTRY_ORG') as HTMLInputElement).value).toBe('byfrederick');
    // A secret shows in a list, not as text: there is no plain input for GH_TOKEN.
    expect(screen.queryByLabelText('Value of GH_TOKEN')).toBeNull();
    expect(screen.getByRole('combobox', { name: 'Secret for GH_TOKEN' }).textContent).toContain('github-token');
    expect(
      screen.getByText('None. A secret can be written to a path, such as a kubeconfig or an SSH key.'),
    ).toBeTruthy();
    expect(screen.getByText('linear')).toBeTruthy();
  });

  it('remove a tool and keep the file’s comments', () => {
    renderHarness();
    fireEvent.click(screen.getByRole('button', { name: 'Remove go@1.26' }));
    expect(screen.queryByText('go@1.26')).toBeNull();
    expect(fileText()).toContain('# what the box installs');
    expect(fileText()).toContain('gh@2');
  });

  it('add a setup command when the field is left', () => {
    renderHarness();
    fireEvent.click(screen.getByRole('button', { name: /Add a command/ }));
    const input = screen.getByLabelText('New setup command');
    fireEvent.change(input, { target: { value: 'task build' } });
    fireEvent.blur(input);
    expect(readEnv(fileText()).setup).toEqual(['pnpm install --frozen-lockfile', 'task build']);
  });

  it('write a changed plain value, and refuse a variable name that is not one', () => {
    renderHarness();
    const value = screen.getByLabelText('Value of SENTRY_ORG');
    fireEvent.change(value, { target: { value: 'acme' } });
    fireEvent.blur(value);
    expect(readEnv(fileText()).env.SENTRY_ORG).toBe('acme');

    const name = screen.getByLabelText('Name of SENTRY_ORG');
    fireEvent.change(name, { target: { value: '9bad' } });
    fireEvent.blur(name);
    expect(screen.getByRole('alert').textContent).toContain('Letters, digits and underscores');
    expect(Object.keys(readEnv(fileText()).env)).toContain('SENTRY_ORG');
  });

  it('show what a test found and set a group of tools to Ask first', () => {
    renderHarness({ results: { linear } });
    expect(screen.getAllByRole('status').map((s) => s.textContent)).toContain('linear: 4 found');
    expect(screen.getByText('4 found')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /linear/ }));
    const group = screen.getByRole('group', { name: 'Change tools of linear' });
    fireEvent.click(within(group).getByRole('radio', { name: 'Ask first' }));
    expect(readEnv(fileText()).mcp.linear?.tools.create_comment).toBe('ask');
    // The Read group is untouched: nothing is written for tools nobody changed.
    expect(readEnv(fileText()).mcp.linear?.tools.list_issues).toBeUndefined();
  });

  it('say a server was not tested and shows no tool list', () => {
    renderHarness();
    expect(screen.getByText('Not tested')).toBeTruthy();
  });

  it('only show the values in view only', () => {
    renderHarness({ readOnly: true });
    expect(screen.queryByRole('button', { name: 'Remove go@1.26' })).toBeNull();
    expect(screen.queryByRole('button', { name: /Add a command/ })).toBeNull();
  });
});
