// @vitest-environment jsdom
// Try with a past event as a person uses it: the dialog opens from the header button, the person picks one of
// the trigger's events and runs the try, and the result says what the trigger would do and that nothing was
// created. The API boundary is mocked; the query client and the dialog are real.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../api/client';
import * as intake from '../../api/intake';
import type { IntakeEvent, Problem, TriggerTry } from '../../api/types';
import { TryCheck, TryWithPastEvent } from './trigger-try';

vi.mock('../../api/intake', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/intake')>()),
  tryIntakeTrigger: vi.fn(),
  tryTriggerCheck: vi.fn(),
}));

const tryIntakeTrigger = vi.mocked(intake.tryIntakeTrigger);
const tryTriggerCheck = vi.mocked(intake.tryTriggerCheck);

const events: IntakeEvent[] = [
  {
    id: 'firing-s1',
    title: '2026-10-26 09:00 UTC',
    event: 'schedule',
    at: '2026-10-26T09:00:00Z',
    outcome: 'created',
    item_number: 151,
    reason: null,
    missed: 0,
  },
  {
    id: 'firing-s2',
    title: '2026-10-19 09:00 UTC',
    event: 'schedule',
    at: '2026-10-19T09:00:00Z',
    outcome: 'skipped',
    item_number: null,
    reason: 'once',
    missed: 0,
  },
];

const wouldCreate: TriggerTry = {
  firing: 'firing-s1',
  matched: true,
  outcome: 'would_create',
  reason: null,
  steps: [
    { step: 'when', outcome: 'passed', reason: null },
    { step: 'only.dedupe', outcome: 'passed', reason: null },
    { step: 'create', outcome: 'passed', reason: null },
    { step: 'limits.perDay', outcome: 'not_reached', reason: null },
  ],
  item: {
    type: 'fix',
    station: 'build',
    lane: null,
    expedite: false,
    title: 'Update the audited dependency',
    brief: 'Bump the package named in the audit.\nSay which advisory it fixes.',
  },
  model_check: null,
};

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderTry(props: Partial<Parameters<typeof TryWithPastEvent>[0]> = {}) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <TryWithPastEvent
        org="org-1"
        factory="factory-1"
        trigger="dependency-audit"
        events={events}
        disabled={false}
        {...props}
      />
    </QueryClientProvider>,
  );
}

const openDialog = () => fireEvent.click(screen.getByRole('button', { name: 'Try with a past event' }));
const runTry = () => fireEvent.click(screen.getByRole('button', { name: 'Run the try' }));

describe('the try', () => {
  it('runs the chosen event and shows what the trigger would do, and that nothing was created', async () => {
    tryIntakeTrigger.mockResolvedValue(wouldCreate);
    renderTry();
    openDialog();

    const dialog = await screen.findByRole('dialog', { name: 'Try with a past event' });
    expect(
      screen.getByText('Runs the saved trigger against one of its recent events. Nothing is created.'),
    ).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Event'), { target: { value: 'firing-s1' } });
    runTry();

    expect(await screen.findByRole('heading', { name: 'The event matched' })).toBeTruthy();
    expect(tryIntakeTrigger).toHaveBeenCalledWith('org-1', 'factory-1', 'dependency-audit', 'firing-s1');
    expect(screen.getByText('The trigger would create an item.')).toBeTruthy();

    const steps = within(dialog).getAllByRole('listitem');
    expect(steps.map((li) => li.textContent)).toEqual([
      'The event matches WhenPassed',
      'No open item already covers itPassed',
      'Create the itemPassed',
      'Under the daily limitNot reached',
    ]);

    expect(within(dialog).getByText('Update the audited dependency')).toBeTruthy();
    expect(within(dialog).getByText('Default lane')).toBeTruthy();
    expect(within(dialog).getByText('Nothing was created. The try changed no item, event or limit.')).toBeTruthy();
  });

  it('shows the reason a firing would be skipped', async () => {
    tryIntakeTrigger.mockResolvedValue({
      ...wouldCreate,
      matched: true,
      outcome: 'skipped',
      reason: 'dedupe: an open item covers this',
      item: null,
      steps: [
        { step: 'when', outcome: 'passed', reason: null },
        { step: 'only.dedupe', outcome: 'skipped', reason: 'an open item covers this' },
        { step: 'create', outcome: 'not_reached', reason: null },
      ],
    });
    renderTry();
    openDialog();
    fireEvent.click(await screen.findByRole('button', { name: 'Run the try' }));
    expect(await screen.findByText('The trigger would skip it. dedupe: an open item covers this')).toBeTruthy();
    expect(screen.getByText('an open item covers this')).toBeTruthy();
    expect(screen.queryByText(/model said/)).toBeNull();
  });

  it('shows text from the event as text, never as markup', async () => {
    const hostile = '<img src=x onerror="alert(1)"> <b>bold</b>';
    tryIntakeTrigger.mockResolvedValue({
      ...wouldCreate,
      item: wouldCreate.item && { ...wouldCreate.item, title: hostile, brief: hostile },
    });
    renderTry();
    openDialog();
    runTry();
    const titles = await screen.findAllByText(hostile);
    expect(titles).toHaveLength(2);
    expect(document.querySelector('img[src="x"]')).toBeNull();
    expect(document.querySelector('b')).toBeNull();
  });

  it('shows a loading state while the try runs', async () => {
    tryIntakeTrigger.mockReturnValue(new Promise(() => {}));
    renderTry();
    openDialog();
    runTry();
    expect(await screen.findByText('Trying the event')).toBeTruthy();
    expect(screen.getByText('Loading')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Trying…' }).hasAttribute('disabled')).toBe(true);
  });

  it('shows the problem detail when the try fails', async () => {
    const problem: Problem = {
      title: 'Not Found',
      status: 404,
      detail: 'firing firing-9 is not one of the 20 newest events',
      code: 'firing-not-found',
    };
    tryIntakeTrigger.mockRejectedValue(new ApiError(problem));
    renderTry();
    openDialog();
    runTry();
    expect(
      await screen.findByText('Could not try the trigger. firing firing-9 is not one of the 20 newest events'),
    ).toBeTruthy();
  });

  it('explains that the events are not stored when the API answers event-not-stored', async () => {
    tryIntakeTrigger.mockRejectedValue(
      new ApiError({ title: 'Unprocessable Entity', status: 422, code: 'event-not-stored' }),
    );
    renderTry();
    openDialog();
    runTry();
    expect(
      await screen.findByText('This event is no longer stored, so it cannot be tried. A delivery is kept for 30 days.'),
    ).toBeTruthy();
    expect(screen.queryByText(/Could not try/)).toBeNull();
  });

  it('leaves no result behind when the dialog is closed and opened again', async () => {
    tryIntakeTrigger.mockResolvedValue(wouldCreate);
    renderTry();
    openDialog();
    runTry();
    await screen.findByRole('heading', { name: 'The event matched' });
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    openDialog();
    expect(await screen.findByRole('dialog')).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'The event matched' })).toBeNull();
  });

  it('is disabled, with no dialog, when the page says the try is unavailable', () => {
    renderTry({ disabled: true });
    expect(screen.getByRole('button', { name: 'Try with a past event' }).hasAttribute('disabled')).toBe(true);
  });
});

describe('Try with a delivery', () => {
  it('names the button as the page says and runs the same try', async () => {
    tryIntakeTrigger.mockResolvedValue(wouldCreate);
    renderTry({ title: 'Try with a delivery' });
    fireEvent.click(screen.getByRole('button', { name: 'Try with a delivery' }));
    runTry();
    expect(await screen.findByRole('heading', { name: 'The event matched' })).toBeTruthy();
    expect(tryIntakeTrigger).toHaveBeenCalledWith('org-1', 'factory-1', 'dependency-audit', 'firing-s1');
  });
});

describe('Try a check', () => {
  const queued = { id: 'c1', state: 'queued', try: true } as never;

  function renderCheck(props: Partial<Parameters<typeof TryCheck>[0]> = {}) {
    const onProblem = vi.fn();
    const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    const view = render(
      <QueryClientProvider client={client}>
        <TryCheck
          org="org-1"
          factory="factory-1"
          trigger="new-incidents"
          disabled={false}
          checking={false}
          readAt={1}
          onProblem={onProblem}
          {...props}
        />
      </QueryClientProvider>,
    );
    return { onProblem, view };
  }

  it('queues a try and reads the log again', async () => {
    tryTriggerCheck.mockResolvedValue(queued);
    const { onProblem } = renderCheck();
    fireEvent.click(screen.getByRole('button', { name: 'Try a check' }));
    await waitFor(() => expect(tryTriggerCheck).toHaveBeenCalledWith('org-1', 'factory-1', 'new-incidents'));
    expect(onProblem).toHaveBeenCalledWith(null);
  });

  it('turns the button off with the reason when a check is still running (409)', async () => {
    tryTriggerCheck.mockRejectedValue(new ApiError({ title: 'Conflict', status: 409, code: 'still-checking' }));
    const { onProblem } = renderCheck();
    fireEvent.click(screen.getByRole('button', { name: 'Try a check' }));
    await waitFor(() =>
      expect(onProblem).toHaveBeenCalledWith('A check is queued or running. Try again when it is done.'),
    );
    expect(screen.getByRole('button', { name: 'Try a check' }).hasAttribute('disabled')).toBe(true);
  });

  it('shows the problem when the trigger has no check (422)', async () => {
    tryTriggerCheck.mockRejectedValue(
      new ApiError({ title: 'Unprocessable', status: 422, code: 'no-check', detail: 'This trigger has no check.' }),
    );
    const { onProblem } = renderCheck();
    fireEvent.click(screen.getByRole('button', { name: 'Try a check' }));
    await waitFor(() => expect(onProblem).toHaveBeenCalledWith('This trigger has no check.'));
  });

  it('is off while a check is queued or running, or the page says so', () => {
    renderCheck({ checking: true });
    expect(screen.getByRole('button', { name: 'Try a check' }).hasAttribute('disabled')).toBe(true);
    cleanup();
    renderCheck({ disabled: true });
    expect(screen.getByRole('button', { name: 'Try a check' }).hasAttribute('disabled')).toBe(true);
  });
});
