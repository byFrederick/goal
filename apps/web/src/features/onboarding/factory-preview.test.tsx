// @vitest-environment jsdom

import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { OrgSummary } from '../../api/types';
import { organizationCard } from './factory-preview';

afterEach(cleanup);

const org: OrgSummary = {
  id: 'org-1',
  slug: 'acme-2026-11-07',
  name: 'Acme',
  mark: 'A',
  factory_count: 0,
  role: 'owner',
};

describe('the organization card', () => {
  it('shows the full address and lets it break only after a slash', () => {
    const { container, getByText } = render(<>{organizationCard(org, 'goal.example.com')}</>);

    const address = getByText(/goal\.example\.com/);
    expect(address.textContent).toBe('goal.example.com/o/acme-2026-11-07');
    expect(address.className).not.toContain('break-all');
    const breaks = [...container.querySelectorAll('wbr')];
    expect(breaks).toHaveLength(2);
    for (const wbr of breaks) expect(wbr.previousSibling?.textContent?.endsWith('/')).toBe(true);
  });
});
