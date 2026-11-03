// A connection as the API answers it, for tests that still read connections (the intake dialog's
// kinds). Used by tests only.
import type { Connection } from '../api/types';

/** A connection as the API answers it: no secret value, only the names set. */
export function connectionFixture(over: Partial<Connection> & Pick<Connection, 'name' | 'kind'>): Connection {
  return {
    id: `id-${over.name}`,
    description: '',
    fields: {},
    secrets_set: [],
    status: 'untested',
    used_by: { factories: [], knowledge_sources: [] },
    last_used_at: null,
    last_test: null,
    tools: [],
    waiting_approvals: 0,
    created_by: { kind: 'user', id: 'user-1', name: 'Freddy Cruz' },
    created_at: '2026-10-01T00:00:00Z',
    updated_by: { kind: 'user', id: 'user-1', name: 'Freddy Cruz' },
    updated_at: '2026-10-01T00:00:00Z',
    ...over,
  } as Connection;
}
