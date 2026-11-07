// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ModelKeyCard } from './model-key-card';

vi.mock(import('../factory-editor/draft/use-draft'), async (original) => ({
  ...(await original()),
  useDraftSession: () => ({ getSnapshot: () => ({ files: new Map() }), editFile: vi.fn() }) as never,
}));
vi.mock(import('../factory-editor/environments/use-environments'), async (original) => ({
  ...(await original()),
  useFileText: () => ({ text: 'apiVersion: goal/v1\nspec:\n  runtime: {}\n', ready: true }) as never,
}));

afterEach(cleanup);

describe('ModelKeyCard', () => {
  it('shows "Model key" once and still names its field', () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <ModelKeyCard org="org-1" factory="factory-1" />
      </QueryClientProvider>,
    );
    expect(screen.getAllByText('Model key')).toHaveLength(1);
    const field = screen.getByLabelText('Model key', { selector: 'input' });
    expect(field.getAttribute('type')).toBe('password');
  });
});
