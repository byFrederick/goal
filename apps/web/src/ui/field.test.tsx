// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Field, Input, NativeSelect } from './field';

afterEach(cleanup);

describe('Field', () => {
  it('shakes the field once and fades the message in when the value is invalid', () => {
    render(
      <Field label="Connection name" error="Use letters, numbers and dashes only.">
        <Input />
      </Field>,
    );

    const input = screen.getByRole('textbox', { name: 'Connection name' });
    expect(input.getAttribute('aria-invalid')).toBe('true');
    expect(input.closest('div')?.classList.contains('motion-safe:animate-error-shake')).toBe(true);
    expect(screen.getByText('Use letters, numbers and dashes only.').classList.contains('animate-fade-in')).toBe(true);
  });

  it('does not shake a field that is valid', () => {
    render(
      <Field label="Connection name">
        <Input />
      </Field>,
    );

    const input = screen.getByRole('textbox', { name: 'Connection name' });
    expect(input.getAttribute('aria-invalid')).toBeNull();
    expect(input.closest('div')?.classList.contains('motion-safe:animate-error-shake')).toBe(false);
  });

  it('draws inputs and selects 32 px tall at every width', () => {
    render(
      <>
        <Input aria-label="Name" />
        <NativeSelect aria-label="Kind" />
      </>,
    );
    for (const name of ['Name', 'Kind']) {
      const classes = Array.from(screen.getByLabelText(name).classList);
      expect(classes).toContain('h-8');
      expect(classes).not.toEqual(expect.arrayContaining(['h-11']));
      expect(classes).not.toContain('md:h-10');
    }
  });
});
