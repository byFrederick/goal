// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getDraftFile, openDraft, publishDraft, putDraftFile } from '../../api/definitions';
import { OpenDraftError, setTriggerEnabled, setTriggersEnabled } from './set-trigger-enabled';
import { readTriggers } from './trigger-yaml';

vi.mock('../../api/definitions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api/definitions')>()),
  openDraft: vi.fn(),
  getDraftFile: vi.fn(),
  putDraftFile: vi.fn(),
  publishDraft: vi.fn(),
}));

const yaml = `spec:
  board:
    triggers:
      - name: flaky-test-on-main
        on: github.check_run.failed
        create: { type: fix, station: build }
      - name: dependency-audit
        on: schedule
        create: { type: infra, station: spec }
      - name: deploy-failed
        enabled: false
        on: webhook.cloudflare.deploy.failed
        create: { type: fix, station: spec }
`;

const base = { org: 'org-1', factory: 'factory-1', currentVersionId: 'version-3' };

beforeEach(() => {
  vi.mocked(openDraft).mockResolvedValue({ revision: 1, change_count: 0 } as never);
  vi.mocked(getDraftFile).mockResolvedValue({ text: yaml, sha256: 'sha-1' } as never);
  vi.mocked(putDraftFile).mockResolvedValue({ revision: 2 } as never);
  vi.mocked(publishDraft).mockResolvedValue({ id: 'version-4' } as never);
});

afterEach(() => {
  vi.clearAllMocks();
});

/** The text the one save wrote. */
function savedText(): string {
  return vi.mocked(putDraftFile).mock.calls[0]?.[3] as string;
}

describe('setTriggersEnabled', () => {
  it('turns on several triggers with one draft, one save and one publish', async () => {
    await setTriggersEnabled({ ...base, triggers: ['flaky-test-on-main', 'dependency-audit'], enabled: true });

    expect(openDraft).toHaveBeenCalledTimes(1);
    expect(putDraftFile).toHaveBeenCalledTimes(1);
    expect(publishDraft).toHaveBeenCalledTimes(1);
    const enabled = readTriggers(savedText()).map((t) => [t.name, t.enabled]);
    expect(enabled).toEqual([
      ['flaky-test-on-main', true],
      ['dependency-audit', true],
      ['deploy-failed', false],
    ]);
  });

  it('keeps the file’s comments and the other triggers as they were', async () => {
    const commented = `# Keep this comment.\n${yaml}`;
    vi.mocked(getDraftFile).mockResolvedValue({ text: commented, sha256: 'sha-1' } as never);
    await setTriggersEnabled({ ...base, triggers: ['flaky-test-on-main', 'dependency-audit'], enabled: true });

    expect(savedText().startsWith('# Keep this comment.\n')).toBe(true);
    expect(savedText()).toContain('on: webhook.cloudflare.deploy.failed');
  });

  it('publishes against the version the switch read', async () => {
    await setTriggersEnabled({ ...base, triggers: ['dependency-audit'], enabled: true });

    expect(publishDraft).toHaveBeenCalledWith(
      'org-1',
      'factory-1',
      expect.objectContaining({ revision: 2, expected_current_version_id: 'version-3' }),
    );
  });

  it('names every trigger in the publish message', async () => {
    await setTriggersEnabled({ ...base, triggers: ['flaky-test-on-main', 'dependency-audit'], enabled: true });

    expect(publishDraft).toHaveBeenCalledWith(
      'org-1',
      'factory-1',
      expect.objectContaining({ message: 'Turn on the triggers flaky-test-on-main, dependency-audit' }),
    );
  });

  it('refuses a draft that already holds someone’s edits, and writes nothing', async () => {
    vi.mocked(openDraft).mockResolvedValue({ revision: 3, change_count: 2 } as never);

    await expect(setTriggersEnabled({ ...base, triggers: ['dependency-audit'], enabled: true })).rejects.toBeInstanceOf(
      OpenDraftError,
    );
    expect(putDraftFile).not.toHaveBeenCalled();
    expect(publishDraft).not.toHaveBeenCalled();
  });
});

describe('setTriggerEnabled', () => {
  it('still turns one trigger off with its own message', async () => {
    await setTriggerEnabled({ ...base, trigger: 'dependency-audit', enabled: false });

    expect(readTriggers(savedText()).find((t) => t.name === 'dependency-audit')?.enabled).toBe(false);
    expect(publishDraft).toHaveBeenCalledWith(
      'org-1',
      'factory-1',
      expect.objectContaining({ message: 'Turn off the trigger dependency-audit' }),
    );
  });
});
