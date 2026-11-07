// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { TooltipProvider } from '../ui/tooltip';
import { CiAccessLine, waitsForCi } from './ci-access-line';

afterEach(cleanup);

const show = (ci: Parameters<typeof CiAccessLine>[0]['ci'], waits: boolean) =>
  render(
    <TooltipProvider>
      <CiAccessLine ci={ci} waitsForCi={waits} />
    </TooltipProvider>,
  );

describe('CiAccessLine', () => {
  it('says quietly that the token can read CI, with no ⓘ', () => {
    show({ ok: true, reason: 'GitHub shows the token the check runs' }, true);
    const line = screen.getByText('The token can read CI.');
    expect(line.className).toContain('text-muted');
    expect(screen.queryByRole('button', { name: 'About reading CI' })).toBeNull();
  });

  it('shows the host’s reason and the token step behind an ⓘ when it fails and a station waits', () => {
    show({ ok: false, reason: 'GitLab shows the token no pipelines: the token needs read_api' }, true);
    const line = screen.getByText(/Can’t read CI\. GitLab shows the token no pipelines/);
    expect(line.className).toContain('text-rework-ink');
    const tip = screen.getByRole('button', { name: 'About reading CI' });
    expect(tip.getAttribute('aria-describedby')).toBeTruthy();
    expect(document.body.textContent).toContain('Checks and Commit statuses');
    expect(document.body.textContent).toContain('read_api');
  });

  it('stays quiet when it fails and no station waits for CI', () => {
    show({ ok: false, reason: 'No access.' }, false);
    const line = screen.getByText(/Can’t read CI\. No access\./);
    expect(line.className).toContain('text-muted');
    expect(line.className).not.toContain('text-rework-ink');
  });

  it('shows nothing when the result has no ci', () => {
    const { container } = show(undefined, true);
    expect(container.textContent).toBe('');
  });
});

describe('waitsForCi', () => {
  it('is true for the string and the object form, false for none', () => {
    expect(waitsForCi({ stations: [{ id: 'a', kind: 'agent', waitFor: 'ci' }] } as never)).toBe(true);
    expect(waitsForCi({ stations: [{ id: 'a', kind: 'agent', waitFor: { ci: { max: '30m' } } }] } as never)).toBe(true);
    expect(waitsForCi({ stations: [{ id: 'a', kind: 'agent' }] } as never)).toBe(false);
    expect(waitsForCi(undefined)).toBe(false);
  });
});
