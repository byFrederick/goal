// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Line, type LineStation } from './line';

const stations: LineStation[] = [
  { id: 'intake', name: 'Intake', count: 4, load: null, waits: ['4 waiting', 'Oldest 3 h'], slots: null },
  {
    id: 'build',
    name: 'Build',
    count: 8,
    load: 0.94,
    constraint: true,
    waits: ['2 ready in buffer'],
    slots: { total: 6, busy: 5, waiting: 1 },
    slotsLabel: 'backend-engineer: 5 working, 1 waiting on you, 0 free',
    share: 'Busy 94%',
  },
  { id: 'done', name: 'Done', count: 7, load: null, done: true, waits: ['7 merged today'], slots: null },
];

const pull = { reason: 'Answers cite the wrong runbook', pulledBy: 'a person' };

function stationItems(): HTMLElement[] {
  return within(screen.getByRole('list', { name: 'Stations from intake to done' })).getAllByRole('listitem');
}

afterEach(cleanup);

describe('Line', () => {
  it('shows the stations with their counts and the constraint marked by name', () => {
    render(<Line title="The line, now" stations={stations} />);

    const line = screen.getByRole('region', { name: 'The line, now' });
    expect(within(line).getByRole('list', { name: 'Stations from intake to done' })).toBeTruthy();
    expect(within(line).getByText('Constraint')).toBeTruthy();
    expect(within(line).getByText('2 ready in buffer')).toBeTruthy();
    expect(
      within(line).getByRole('img', { name: 'backend-engineer: 5 working, 1 waiting on you, 0 free' }),
    ).toBeTruthy();
  });

  it('shows the andon strip with its reason and who pulled it, and dims the stations', () => {
    render(
      <Line
        title="The line, now"
        stations={stations}
        andon={{ reason: 'Answers cite the wrong runbook', pulledBy: 'a person' }}
        clear={<button type="button">Clear the andon</button>}
      />,
    );

    const strip = screen.getByRole('status');
    expect(within(strip).getByText('Answers cite the wrong runbook')).toBeTruthy();
    expect(within(strip).getByText('Pulled by a person')).toBeTruthy();
    expect(within(strip).getByRole('button', { name: 'Clear the andon' })).toBeTruthy();
    expect(screen.getByRole('status').getAttribute('data-initial')).not.toBeNull();
    for (const li of stationItems()) expect(li.getAttribute('data-dimmed')).toBe('true');
  });

  it('shows no strip and leaves the stations bright while the line runs', () => {
    render(<Line title="The line, now" stations={stations} andon={null} />);

    expect(screen.queryByRole('status')).toBeNull();
    for (const li of stationItems()) expect(li.getAttribute('data-dimmed')).toBeNull();
  });

  it('slides the strip in and dims each station when the andon is pulled', () => {
    const { rerender } = render(<Line title="The line, now" stations={stations} andon={null} />);
    rerender(<Line title="The line, now" stations={stations} andon={pull} />);

    const strip = screen.getByRole('status');
    expect(strip.getAttribute('data-state')).toBe('open');
    expect(strip.getAttribute('data-initial')).toBeNull();
    expect(strip.className).toContain('motion-panel');
    expect(strip.className).toContain('motion-andon');
    for (const li of stationItems()) {
      expect(li.getAttribute('data-dimmed')).toBe('true');
      expect(li.className).toContain('data-[dimmed=true]:text-muted');
    }
  });

  it('sets the closed state before the strip unmounts on Clear', () => {
    const { rerender, container } = render(<Line title="The line, now" stations={stations} andon={pull} />);
    rerender(<Line title="The line, now" stations={stations} andon={null} />);

    const strip = container.querySelector('.motion-andon') as HTMLElement;
    expect(strip.getAttribute('data-state')).toBe('closed');
    expect(strip.getAttribute('aria-hidden')).toBe('true');
    expect(strip.hasAttribute('inert')).toBe(true);
    expect(strip.textContent).toContain('Answers cite the wrong runbook');
    for (const li of stationItems()) expect(li.getAttribute('data-dimmed')).toBeNull();

    // jsdom has no CSS animation support, so React listens for the prefixed name here.
    fireEvent(strip, new Event('webkitAnimationEnd', { bubbles: true }));
    expect(container.querySelector('.motion-andon')).toBeNull();
  });

  it('shows the strip without an entrance when the line loads stopped', () => {
    render(<Line title="The line, now" stations={stations} andon={pull} />);

    const strip = screen.getByRole('status');
    expect(strip.getAttribute('data-state')).toBe('open');
    expect(strip.getAttribute('data-initial')).not.toBeNull();
  });

  it('puts the count, the name and the slots of a station in one row, in that order', () => {
    render(<Line title="The line, now" stations={stations} />);

    const row = screen.getByText('Build').closest('li');
    expect(row).not.toBeNull();
    const count = within(row as HTMLElement).getAllByText('8')[0] as HTMLElement;
    const name = within(row as HTMLElement).getByText('Build');
    const slots = within(row as HTMLElement).getByRole('img', { name: /backend-engineer/ });
    expect(count.compareDocumentPosition(name) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(name.compareDocumentPosition(slots) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});
