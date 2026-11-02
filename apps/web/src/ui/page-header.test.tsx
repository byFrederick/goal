// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { PageHeader } from './page-header';

afterEach(cleanup);

describe('PageHeader', () => {
  it('lets the title shrink and wrap, and keeps the actions whole', () => {
    render(<PageHeader title="Members" actions={<button type="button">Invite</button>} />);

    const h1 = screen.getByRole('heading', { level: 1, name: 'Members' });
    expect(h1.className).toContain('text-balance');
    expect(h1.className).toContain('break-words');
    expect(h1.parentElement?.className).toContain('min-w-0');
    expect(h1.parentElement?.className).toContain('flex-1');
    expect(screen.getByRole('button', { name: 'Invite' }).parentElement?.className).toContain('shrink-0');
  });
});
