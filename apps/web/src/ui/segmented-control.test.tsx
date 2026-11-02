// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { SegmentedControl } from './segmented-control';

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
      expect.arrayContaining(['transition-[transform,width]', 'duration-(--duration-fast)', 'ease-out']),
    );
    expect(Array.from(radio('All').closest('label')?.classList ?? [])).toEqual(expect.arrayContaining(['text-ink']));
    expect(Array.from(radio('All').closest('label')?.classList ?? [])).not.toContain('bg-card');

    rerender(<SegmentedControl label="View" options={options} value="mine" onChange={() => {}} />);
    expect(container.querySelectorAll('[aria-hidden="true"]')).toHaveLength(1);
    expect(radio('Mine 3').checked).toBe(true);
  });

  it('shows the choice only when readOnly, and does not call onChange', () => {
    const onChange = vi.fn();
    render(<SegmentedControl label="View" options={options} value="mine" onChange={onChange} readOnly />);
    expect(radio('Mine 3').checked).toBe(true);
    expect(radio('Mine 3').disabled).toBe(true);
    expect(radio('All').disabled).toBe(true);
    expect(onChange).not.toHaveBeenCalled();
  });
});
