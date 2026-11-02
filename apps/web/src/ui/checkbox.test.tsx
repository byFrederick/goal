// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Checkbox } from './checkbox';

afterEach(cleanup);

function Choice({ disabled = false, onChange }: { disabled?: boolean; onChange?: (on: boolean) => void }) {
  const [on, setOn] = useState(false);
  return (
    <Checkbox
      label="Select all"
      checked={on}
      disabled={disabled}
      onCheckedChange={(next) => {
        setOn(next);
        onChange?.(next);
      }}
    />
  );
}

const box = () => screen.getByRole('checkbox', { name: 'Select all' }) as HTMLInputElement;

describe('Checkbox', () => {
  it('is a native checkbox named by its visible label', () => {
    render(<Choice />);
    expect(box().type).toBe('checkbox');
    expect(box().checked).toBe(false);
  });

  it('names the checkbox by aria-label when there is no visible label', () => {
    render(<Checkbox checked={false} onCheckedChange={() => {}} aria-label="Select row 4" />);
    expect(screen.getByRole('checkbox', { name: 'Select row 4' })).toBeTruthy();
  });

  it('toggles when its label is clicked', () => {
    const onChange = vi.fn();
    render(<Choice onChange={onChange} />);
    fireEvent.click(screen.getByText('Select all'));
    expect(box().checked).toBe(true);
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it('marks the box checked, which is what the box fill and the check draw from', () => {
    const { container } = render(<Choice />);
    const drawn = container.querySelector('.check-box');
    expect(drawn?.hasAttribute('data-checked')).toBe(false);
    fireEvent.click(box());
    expect(container.querySelector('.check-box')?.hasAttribute('data-checked')).toBe(true);
    expect(Array.from(drawn?.classList ?? [])).toEqual(expect.arrayContaining(['duration-(--duration-quick)']));
  });

  it('does not change when disabled', () => {
    const onChange = vi.fn();
    const { container } = render(<Choice disabled onChange={onChange} />);
    expect(box().disabled).toBe(true);
    fireEvent.click(box());
    expect(container.querySelector('.check-box')?.hasAttribute('data-checked')).toBe(false);
    expect(onChange).not.toHaveBeenCalled();
  });
});
