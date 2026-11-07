// @vitest-environment jsdom

import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useEffect, useState } from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import type { LineSection, ModelStation } from '../../../shared/definition/model';
import { AddStationMenu } from './add-station-menu';
import { type LineEditor, LineEditorContext } from './line-editor';

afterEach(cleanup);

const stations: ModelStation[] = [
  { id: 'inbox', name: 'Inbox', kind: 'queue' },
  { id: 'done', name: 'Done', kind: 'done' },
];

// Stands in for the Line editor: the inserted station's name takes focus, as line-editor's layout effect does.
function Harness({ log }: { log: string[] }) {
  const [added, setAdded] = useState<string | undefined>();
  const line: LineSection = { stations, intake: [] };
  const editor = {
    line,
    stations,
    lastFocused: { current: undefined },
    insert: (_edit: unknown, station: { id: string }) => setAdded(station.id),
  } as unknown as LineEditor;
  useEffect(() => {
    if (added) document.querySelector<HTMLElement>(`[data-station-name="${added}"]`)?.focus();
  }, [added]);
  return (
    <LineEditorContext value={editor}>
      <AddStationMenu />
      {added ? (
        <div data-station={added}>
          <input
            aria-label="Name"
            data-station-name={added}
            onFocus={() => log.push('focus name')}
            onBlur={() => log.push('blur name')}
          />
        </div>
      ) : null}
    </LineEditorContext>
  );
}

describe('Add station', () => {
  it('moves focus once, to the new station name, and leaves it there', async () => {
    const log: string[] = [];
    render(<Harness log={log} />);
    await userEvent.click(screen.getByRole('button', { name: /Add station/ }));
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Agent station' }));
    const name = await screen.findByRole('textbox', { name: 'Name' });
    await waitFor(() => expect(document.activeElement).toBe(name));
    // Let the menu finish closing and the frame after it run.
    await new Promise((resolve) => requestAnimationFrame(() => setTimeout(resolve, 50)));
    expect(document.activeElement).toBe(name);
    expect(log).toEqual(['focus name']);
  });
});
