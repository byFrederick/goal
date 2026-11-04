// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { SegmentedControl } from './segmented-control';
import { TooltipProvider } from './tooltip';

afterEach(cleanup);

const options = [
  { value: 'mine', label: 'Mine', count: 3 },
  { value: 'all', label: 'All' },
] as const;

const radio = (name: string) => screen.getByRole('radio', { name }) as HTMLInputElement;

describe('SegmentedControl', () => {
  it('offers each option as a radio in one group', () => {
    render(<SegmentedControl label="View" options={options} value="all" onChange={() => {}} />);
    expect(screen.getAllByRole('radio')).toHaveLength(2);
    expect(radio('All').checked).toBe(true);
  });

  it('includes the count in the accessible name of its option', () => {
    render(<SegmentedControl label="View" options={options} value="all" onChange={() => {}} />);
    expect(radio('Mine 3').value).toBe('mine');
  });

  it('keeps the disabled reason outside the option label and wired by aria-describedby', () => {
    render(
      <TooltipProvider>
        <SegmentedControl
          label="Kind"
          options={[
            { value: 'agent', label: 'Agent' },
            { value: 'done', label: 'Done', disabledReason: 'Only the last station can be Done' },
          ]}
          value="agent"
          onChange={() => {}}
        />
      </TooltipProvider>,
    );
    const done = radio('Done');
    const reason = document.getElementById(done.getAttribute('aria-describedby') ?? '');
    expect(reason?.textContent).toBe('Only the last station can be Done');
    expect(done.closest('label')?.contains(reason)).toBe(false);
  });

  it('calls onChange with the option that was clicked', () => {
    const onChange = vi.fn();
    render(<SegmentedControl label="View" options={options} value="all" onChange={onChange} />);
    fireEvent.click(radio('Mine 3'));
    expect(onChange).toHaveBeenCalledWith('mine');
  });

  it('moves one sliding pill to the chosen option, with the fast ease-out slide', () => {
    const { container, rerender } = render(
      <SegmentedControl label="View" options={options} value="all" onChange={() => {}} />,
    );
    const pill = container.querySelector('[aria-hidden="true"]');
    expect(Array.from(pill?.classList ?? [])).toEqual(
      expect.arrayContaining(['transition-transform', 'origin-left', 'duration-(--duration-fast)', 'ease-out']),
    );
    expect(pill?.className).not.toMatch(/width/);
    expect((pill as HTMLElement).style.width).toBe('');
    expect((pill as HTMLElement).style.transition).not.toContain('width');
    expect(Array.from(radio('All').closest('label')?.classList ?? [])).toEqual(expect.arrayContaining(['text-ink']));
    expect(Array.from(radio('All').closest('label')?.classList ?? [])).not.toContain('bg-card');

    rerender(<SegmentedControl label="View" options={options} value="mine" onChange={() => {}} />);
    expect(container.querySelectorAll('[aria-hidden="true"]')).toHaveLength(1);
    expect(radio('Mine 3').checked).toBe(true);
  });

  it('moves the choice with the arrow keys, as in any radio group', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<SegmentedControl label="View" options={options} value="all" onChange={onChange} />);
    radio('All').focus();
    await user.keyboard('{ArrowLeft}');
    expect(onChange).toHaveBeenCalledWith('mine');
  });

  it('shows the choice only when readOnly, and does not call onChange', () => {
    const onChange = vi.fn();
    render(<SegmentedControl label="View" options={options} value="mine" onChange={onChange} readOnly />);
    expect(radio('Mine 3').checked).toBe(true);
    expect(radio('Mine 3').disabled).toBe(true);
    expect(radio('All').disabled).toBe(true);
    expect(onChange).not.toHaveBeenCalled();
  });

  it('keeps the default heights unless the compact size is asked for', () => {
    const { rerender } = render(<SegmentedControl label="View" options={options} value="mine" onChange={() => {}} />);
    const label = () => screen.getByRole('radio', { name: 'All' }).closest('label');
    expect(label()?.className).toContain('md:h-7.5');
    rerender(<SegmentedControl label="View" options={options} value="mine" onChange={() => {}} size="compact" />);
    expect(label()?.className).toContain('md:h-7');
    expect(label()?.className).toContain('h-11');
  });
});

describe('SegmentedControl scrolling', () => {
  beforeEach(() => {
    Element.prototype.scrollIntoView ??= () => {};
  });

  it('scrolls the chosen option into view without smooth behavior', () => {
    const scroll = vi
      .spyOn(Element.prototype as { scrollIntoView(o?: unknown): void }, 'scrollIntoView')
      .mockImplementation(() => {});
    render(<SegmentedControl label="View" options={options} value="all" onChange={() => {}} />);
    expect(scroll).toHaveBeenCalledWith({ block: 'nearest', inline: 'nearest' });
    scroll.mockRestore();
  });

  it('scrolls sideways inside its own box', () => {
    render(<SegmentedControl label="View" options={options} value="all" onChange={() => {}} />);
    expect(screen.getByRole('group', { name: 'View' }).className).toContain('overflow-x-auto');
  });
});
