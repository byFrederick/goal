// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { keys } from '../../../api/queries';
import type { DefinitionProblem } from '../../../api/types';
import { readEnv } from './env-file';
import { ChecksCard, InlineProblems, useFixes } from './environment-checks';

const problems: DefinitionProblem[] = [
  {
    code: 'plain-key',
    severity: 'error',
    check: 'secrets',
    section: 'environments',
    pointer: '/environments/default/env/GH_TOKEN',
    file: 'environments/default.yaml',
    line: 7,
    column: 1,
    end_line: 7,
    end_column: 0,
    message: 'GH_TOKEN holds a key in plain text.',
    // internal/authoring/secrets_test.go: the fix's path is /<environment>/<keys below spec>.
    fixes: [
      {
        label: 'Move to Secrets',
        section: 'environments',
        patch: [{ op: 'replace', path: '/default/env/GH_TOKEN', value: '{{gh-token}}' }],
      },
    ],
  },
  {
    ...({} as DefinitionProblem),
    code: 'x',
    severity: 'error',
    check: 'c',
    section: 'line',
    message: 'not here',
    fixes: [],
    line: 0,
    column: 0,
    end_line: 0,
    end_column: 0,
  },
];

vi.mock('../frame/use-editor', () => ({ useValidation: () => ({ problems }) }));
vi.mock('../../../shared/org', () => ({ useOrg: () => ({ id: 'o1', slug: 'acme', name: 'Acme' }) }));
// The sheet is the Secrets page's own; here it offers to create the secret and says what it was given.
vi.mock('../../secrets/new-secret-sheet', () => ({
  NewSecretSheet: ({
    open,
    initial,
    onCreated,
  }: {
    open: boolean;
    initial?: { name?: string; value?: string };
    onCreated?: (name: string) => void;
  }) =>
    open ? (
      <div role="dialog" aria-label="New secret">
        <span>{`name ${initial?.name} value ${initial?.value}`}</span>
        <button type="button" onClick={() => onCreated?.(initial?.name ?? '')}>
          Create secret
        </button>
      </div>
    ) : null,
}));

const file =
  'apiVersion: goal/v1\nkind: Environment\nspec:\n  env:\n    GH_TOKEN: ghp_0123456789abcdef # keep\n  mcp:\n    linear:\n      url: https://mcp.linear.app/mcp\n';

function Harness({
  secrets,
  testing = false,
  testAll = () => undefined,
}: {
  secrets: string[];
  testing?: boolean;
  testAll?: () => void;
}) {
  const [text, setText] = useState(file);
  const spec = readEnv(text);
  const change = (edit: (t: string) => string) => setText((t) => edit(t));
  const fixes = useFixes({ spec, change });
  return (
    <>
      <InlineProblems environment="default" apply={fixes.apply} readOnly={false} />
      <ChecksCard
        environment="default"
        spec={spec}
        apply={fixes.apply}
        readOnly={false}
        results={{}}
        factoryName="goal"
        testAll={testAll}
        testing={testing}
      />
      {fixes.sheet}
      <pre data-testid="file">{text}</pre>
      <span hidden>{secrets.join()}</span>
    </>
  );
}

function renderChecks(secrets: string[], props: { testing?: boolean; testAll?: () => void } = {}) {
  const client = new QueryClient();
  client.setQueryData(
    keys.secrets('o1'),
    secrets.map((name) => ({ name })),
  );
  client.setQueryData(keys.factories('o1'), []);
  return render(
    <QueryClientProvider client={client}>
      <Harness secrets={secrets} {...props} />
    </QueryClientProvider>,
  );
}

afterEach(cleanup);

describe('the checks of an environment', () => {
  it('list the problems of its own file and no others', () => {
    renderChecks([]);
    expect(screen.getAllByText('GH_TOKEN holds a key in plain text.').length).toBeGreaterThan(0);
    expect(screen.queryByText('not here')).toBeNull();
    expect(screen.getByText('Checks on default')).toBeTruthy();
  });

  it('show Test servers as busy, not disabled, while the servers are tested, and ignore a click', () => {
    const testAll = vi.fn();
    renderChecks([], { testing: true, testAll });
    const button = screen.getByRole('button', { name: 'Test servers' }) as HTMLButtonElement;
    expect(button.getAttribute('aria-busy')).toBe('true');
    expect(button.getAttribute('aria-disabled')).toBe('true');
    expect(button.disabled).toBe(false);
    fireEvent.click(button);
    expect(testAll).not.toHaveBeenCalled();
  });

  it('create the secret in place for Move to Secrets, then write its reference', () => {
    renderChecks([]);
    fireEvent.click(screen.getAllByRole('button', { name: 'Move to Secrets' })[0] as HTMLElement);
    // The sheet gets the suggested name and the value that sat in the file.
    expect(screen.getByRole('dialog').textContent).toBe('name gh-token value ghp_0123456789abcdefCreate secret');
    expect(screen.getByTestId('file').textContent).toContain('ghp_0123456789abcdef');
    fireEvent.click(screen.getByRole('button', { name: 'Create secret' }));
    const text = screen.getByTestId('file').textContent ?? '';
    expect(readEnv(text).env.GH_TOKEN).toBe('{{gh-token}}');
    expect(text).toContain('# keep');
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('write the reference at once when the secret already exists', () => {
    renderChecks(['gh-token']);
    fireEvent.click(screen.getAllByRole('button', { name: 'Move to Secrets' })[0] as HTMLElement);
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(readEnv(screen.getByTestId('file').textContent ?? '').env.GH_TOKEN).toBe('{{gh-token}}');
  });
});
