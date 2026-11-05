// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { listInDraft, listInDrafts } from './draft';
import { FactoryCheckboxes } from './factory-checkboxes';

const files = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), open: vi.fn(async () => ({})), publish: vi.fn() }));
vi.mock('../../api/definitions', () => ({
  getDraftFile: files.get,
  openDraft: files.open,
  putDraftFile: files.put,
  publishDraft: files.publish,
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const factories = [
  { id: 'f-sre', name: 'SRE' },
  { id: 'f-software', name: 'Software' },
];

function Harness({ onPick }: { onPick: (ids: string[]) => void }) {
  const [ids, setIds] = useState<string[]>([]);
  return (
    <FactoryCheckboxes
      factories={factories}
      checked={ids}
      onChange={(next) => {
        setIds(next);
        onPick(next);
      }}
    />
  );
}

const draft = '# the factory\nspec:\n  knowledge:\n    sources:\n      - handbook # kept\n';

describe('the factory checkboxes', () => {
  it('check and uncheck a factory and say what saving does for each', async () => {
    const picked = vi.fn();
    render(<Harness onPick={picked} />);

    expect(screen.getAllByText('Not listed')).toHaveLength(2);
    await userEvent.click(screen.getByRole('checkbox', { name: /SRE/ }));
    expect(picked).toHaveBeenLastCalledWith(['f-sre']);
    expect(screen.getByText('Into its draft')).toBeTruthy();

    await userEvent.click(screen.getByRole('checkbox', { name: /SRE/ }));
    expect(picked).toHaveBeenLastCalledWith([]);
  });
});

describe('listing a source in a factory’s draft', () => {
  it('appends it once, keeps the file’s comments and never publishes', async () => {
    files.get.mockResolvedValue({ text: draft, sha256: 'abc' });
    files.put.mockResolvedValue({});

    const results = await listInDrafts('org-1', [factories[0] as (typeof factories)[number]], 'runbooks');

    expect(results).toEqual([{ factory: 'SRE', outcome: 'added' }]);
    expect(files.put).toHaveBeenCalledTimes(1);
    const [org, factory, path, text, base] = files.put.mock.calls[0] ?? [];
    expect([org, factory, path, base]).toEqual(['org-1', 'f-sre', 'factory.yaml', { sha256: 'abc' }]);
    expect(text).toContain('- handbook # kept');
    expect(text).toContain('- runbooks');
    expect(text.match(/runbooks/g)).toHaveLength(1);
    expect(files.publish).not.toHaveBeenCalled();
  });

  it('writes nothing when the draft lists it already, as a name or an entry', async () => {
    files.get.mockResolvedValue({
      text: 'spec:\n  knowledge:\n    sources:\n      - name: runbooks\n        readWhen: x\n',
      sha256: 'abc',
    });

    expect(await listInDraft('org-1', 'f-sre', 'runbooks')).toBe(false);
    expect(files.put).not.toHaveBeenCalled();
  });

  it('refuses a draft that does not parse instead of overwriting it', async () => {
    files.get.mockResolvedValue({ text: 'spec: [unclosed', sha256: 'abc' });

    const [result] = await listInDrafts('org-1', [factories[1] as (typeof factories)[number]], 'runbooks');

    expect(result?.outcome).toBe('failed');
    expect(files.put).not.toHaveBeenCalled();
  });

  it('adds the knowledge section to a draft that has none', async () => {
    files.get.mockResolvedValue({ text: 'spec:\n  agents: []\n', sha256: 'abc' });
    files.put.mockResolvedValue({});

    expect(await listInDraft('org-1', 'f-sre', 'runbooks')).toBe(true);
    expect(files.put.mock.calls[0]?.[3]).toContain('knowledge:');
  });
});
