// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../test/select';
import { choose } from '../test/select';
import { Field } from './field';
import { Select } from './select';

afterEach(cleanup);

const options = [
  { value: '', label: 'All kinds' },
  { value: 'slack', label: 'Slack' },
  { value: 'datadog', label: 'Datadog' },
];

describe('Select', () => {
  it('is a combobox named by its label and shows the chosen option', () => {
    render(
      <Field label="Kind">
        <Select value="slack" onValueChange={() => {}} options={options} />
      </Field>,
    );
    expect(screen.getByRole('combobox', { name: 'Kind' }).textContent).toBe('Slack');
  });

  it('chooses an option with a click and reports its value', async () => {
    const onValueChange = vi.fn();
    render(<Select aria-label="Kind" value="" onValueChange={onValueChange} options={options} />);
    await choose('Kind', 'Datadog');
    expect(onValueChange).toHaveBeenCalledWith('datadog');
  });

  it('reports an empty string for the empty option', async () => {
    const onValueChange = vi.fn();
    render(<Select aria-label="Kind" value="slack" onValueChange={onValueChange} options={options} />);
    await choose('Kind', 'All kinds');
    expect(onValueChange).toHaveBeenCalledWith('');
  });

  it('shows the empty option as chosen when the value is empty', () => {
    render(<Select aria-label="Kind" value="" onValueChange={() => {}} options={options} />);
    expect(screen.getByRole('combobox', { name: 'Kind' }).textContent).toBe('All kinds');
  });

  it('opens with the keyboard, moves with the arrows, chooses with Enter and closes with Escape', async () => {
    const onValueChange = vi.fn();
    render(<Select aria-label="Kind" value="" onValueChange={onValueChange} options={options} />);
    const trigger = screen.getByRole('combobox', { name: 'Kind' });
    trigger.focus();
    fireEvent.keyDown(trigger, { key: 'Enter' });
    const list = await screen.findByRole('listbox');
    const active = () =>
      within(list)
        .getAllByRole('option')
        .find((o) => o.hasAttribute('data-highlighted'));
    expect(active()?.textContent).toBe('All kinds');
    fireEvent.keyDown(document.activeElement ?? list, { key: 'ArrowDown' });
    await waitFor(() => expect(active()?.textContent).toBe('Slack'));
    fireEvent.keyDown(document.activeElement ?? list, { key: 'Enter' });
    expect(onValueChange).toHaveBeenCalledWith('slack');

    fireEvent.keyDown(trigger, { key: 'Enter' });
    await screen.findByRole('listbox');
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
    expect(screen.queryByRole('listbox')).toBeNull();
    await waitFor(() => expect(document.activeElement).toBe(trigger));
  });

  it('cannot be opened when disabled', () => {
    render(<Select aria-label="Kind" disabled value="" onValueChange={() => {}} options={options} />);
    const trigger = screen.getByRole('combobox', { name: 'Kind' });
    expect((trigger as HTMLButtonElement).disabled).toBe(true);
    fireEvent.keyDown(trigger, { key: 'Enter' });
    expect(screen.queryByRole('listbox')).toBeNull();
  });
});
