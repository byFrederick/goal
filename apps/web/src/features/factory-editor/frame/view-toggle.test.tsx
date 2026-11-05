// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ViewToggle } from './view-toggle';

const navigate = vi.hoisted(() => vi.fn());
const search = vi.hoisted(() => ({ value: {} as Record<string, unknown> }));
const placeAtLine = vi.hoisted(() => vi.fn());
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate, useSearch: () => search.value }));
vi.mock('../../../shared/factory', () => ({ useFactoryParams: () => ({ org: 'acme', factory: 'f' }) }));
vi.mock('../../../shared/definition/place', () => ({ placeAtLine }));
vi.mock('../draft/use-draft', () => ({
  useModel: () => ({}),
  useDraft: () => ({}),
}));
vi.mock('../section-views', () => ({ yamlView: () => null }));

afterEach(() => {
  cleanup();
  navigate.mockReset();
  placeAtLine.mockReset();
});

describe('the Form | YAML switch', () => {
  it('has the shown view checked', () => {
    search.value = { mode: 'yaml' };
    render(<ViewToggle section="line" yaml />);
    expect((screen.getByRole('radio', { name: 'YAML' }) as HTMLInputElement).checked).toBe(true);
    expect(screen.getAllByRole('radio', { name: 'YAML' })).toHaveLength(1);
  });

  it('opens Form at the cursor’s line, not the line the URL has caught up to', () => {
    search.value = { mode: 'yaml', line: 5 };
    placeAtLine.mockReturnValue({ section: 'line', station: 'spec' });
    render(<ViewToggle section="basics" yaml cursorLine={12} />);
    fireEvent.click(screen.getByRole('radio', { name: 'Form' }));
    expect(placeAtLine).toHaveBeenCalledWith(expect.anything(), expect.anything(), 'factory.yaml', 12);
    expect(navigate).toHaveBeenCalledWith(
      expect.objectContaining({ params: { org: 'acme', factory: 'f', section: 'line' }, search: { station: 'spec' } }),
    );
  });
});
