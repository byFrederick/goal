// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AgentsSection } from '../../../shared/definition/model';
import { PresetControl } from './model-preset-control';

// A stand-in for the draft session: edits land in `state` the way the real session shows them, and the
// test sets the save outcome (`status`, `error`) the way a save would.
const state: { agents: AgentsSection; status: string; error: unknown } = {
  agents: {},
  status: 'saved',
  error: null,
};
const session = { edit: vi.fn() };
const navigate = vi.fn();

vi.mock('../draft/use-draft', () => ({
  useSection: () => state.agents,
  useDraftSession: () => session,
  useDraft: (select: (s: typeof state) => unknown) => select(state),
}));
vi.mock('./line-editor', () => ({ useLineEditor: () => ({ viewOnly: false }) }));
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }));

const draft: AgentsSection = {
  default_model: 'claude-haiku-5-5',
  agents: [{ name: 'architect', model: 'claude-haiku-5-5' }, { name: 'reviewer' }],
};

beforeEach(() => {
  state.agents = draft;
  state.status = 'saved';
  state.error = null;
  session.edit.mockImplementation((edit: { value: AgentsSection }) => {
    state.agents = edit.value;
    state.status = 'saving';
  });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('model preset control', () => {
  it('saves Cost first to the draft and records it in the URL once the save succeeds', () => {
    const { rerender } = render(<PresetControl />);
    fireEvent.click(screen.getByRole('button', { name: 'Cost first' }));

    expect(session.edit).toHaveBeenCalledTimes(1);
    expect(state.agents.default_model).toBe('claude-sonnet-5-5');
    expect(screen.getByRole('button', { name: 'Cost first' }).getAttribute('aria-pressed')).toBe('true');
    expect(navigate).not.toHaveBeenCalled();

    state.status = 'saved';
    rerender(<PresetControl />);
    expect(navigate).toHaveBeenCalledTimes(1);
    const [options] = navigate.mock.calls[0] as [{ search: (prev: object) => object }];
    expect(options.search({ factory: 'f1' })).toEqual({ factory: 'f1', preset: 'cost' });
  });

  it('shows the refused save and puts the previous choice back when the draft write fails', () => {
    const { rerender } = render(<PresetControl />);
    fireEvent.click(screen.getByRole('button', { name: 'Cost first' }));

    state.error = { title: 'Not saved', detail: 'The model is not allowed here' };
    state.status = 'failed';
    rerender(<PresetControl />);

    expect(screen.getByRole('alert').textContent).toContain('The model is not allowed here');
    expect(state.agents).toEqual(draft);
    expect(screen.getByRole('button', { name: 'Cost first' }).getAttribute('aria-pressed')).toBe('false');
    expect(navigate).not.toHaveBeenCalled();
  });

  it('presses neither choice for a hand-set default model', () => {
    state.agents = { default_model: 'claude-haiku-5-5', agents: [] };
    render(<PresetControl />);
    expect(screen.getByRole('button', { name: 'Quality first' }).getAttribute('aria-pressed')).toBe('false');
    expect(screen.getByRole('button', { name: 'Cost first' }).getAttribute('aria-pressed')).toBe('false');
  });
});
