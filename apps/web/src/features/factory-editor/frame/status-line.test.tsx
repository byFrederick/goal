// @vitest-environment jsdom
import { cleanup, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Factory, OrgSummary } from '../../../api/types';
import { renderFactoryPage } from '../../../test/route';
import { StatusLine } from './status-line';

afterEach(cleanup);

const org: OrgSummary = { id: 'org-1', slug: 'acme', name: 'Acme', mark: 'A', factory_count: 1, role: 'owner' };
const factory: Factory = {
  id: 'factory-1',
  org_id: 'org-1',
  name: 'goal',
  title: 'Goal',
  work_types: ['feature'],
  stations: [],
  current: null,
  draft: null,
  paused: false,
  created_at: '2026-10-01T00:00:00Z',
  role: 'editor',
  can: ['definition:edit'],
};

describe('the status line links', () => {
  it('are 44 px tall below md and keep their desktop classes', async () => {
    renderFactoryPage({
      page: () => (
        <StatusLine
          session={{
            status: 'saved',
            savedAt: null,
            retryAt: null,
            error: null,
            updatedElsewhereAt: null,
            hasDraft: true,
            discarded: false,
          }}
          base={{ number: 8 }}
          current={{ number: 9 }}
          changes="3 changes in 2 sections"
          problemCount={0}
          firstProblem={undefined}
          model={{} as never}
          onRetry={vi.fn()}
          onResolve={vi.fn()}
          onRestart={vi.fn()}
        />
      ),
      path: 'definition/line',
      org,
      factory,
    });
    const link = await screen.findByRole('link', { name: '3 changes in 2 sections' });
    for (const c of ['max-md:min-h-11', 'max-md:inline-flex', 'max-md:items-center', 'underline']) {
      expect(link.className).toContain(c);
    }
  });
});
