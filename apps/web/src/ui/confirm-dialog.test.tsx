// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ConfirmDialog } from './confirm-dialog';

afterEach(cleanup);

function renderDialog(props: { pending?: boolean; disabled?: boolean; onConfirm?: () => void }) {
  return render(
    <ConfirmDialog
      open
      onOpenChange={() => undefined}
      title="Delete secret"
      confirmLabel="Delete"
      pendingLabel="Deleting…"
      onConfirm={props.onConfirm ?? (() => undefined)}
      pending={props.pending ?? false}
      disabled={props.disabled ?? false}
    />,
  );
}

describe('ConfirmDialog confirm button', () => {
  it('is busy, not disabled, while pending, and a click does not confirm again', async () => {
    const onConfirm = vi.fn();
    renderDialog({ pending: true, onConfirm });

    const button = await screen.findByRole('button', { name: /Deleting…/ });
    expect(button.getAttribute('aria-busy')).toBe('true');
    expect(button.getAttribute('aria-disabled')).toBe('true');
    expect(button.hasAttribute('disabled')).toBe(false);
    fireEvent.click(button);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('stays disabled when the caller disables it for a reason that is not a pending request', () => {
    renderDialog({ disabled: true });
    expect(screen.getByRole('button', { name: 'Delete' }).hasAttribute('disabled')).toBe(true);
  });
});
