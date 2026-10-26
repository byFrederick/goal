// The decisions a save elsewhere can ask for (D §10.4). (no-auth) "Elsewhere" is another tab of
// the local user or the CLI; phase 3 names the person.
// - ConflictDialog: a field both sides changed, one row each ("Build’s WIP limit also changed
//   elsewhere: theirs 4, yours 2") with Keep theirs / Keep mine, and Keep all mine.
// - FileConflictDialog: a file changed elsewhere, theirs beside yours: Use theirs (yours goes to
//   the clipboard) or Overwrite with mine.
// - DiscardedDialog: the draft was discarded elsewhere: Start a new draft or Leave.
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { draftFileQuery } from '../../../api/queries';
import { type Section, sections } from '../../../shared/definition/model';
import { Button } from '../../../ui/button';
import { Dialog, DialogContent } from '../../../ui/dialog';
import type { SectionConflict } from '../draft/session';
import { useDraft, useDraftSession } from '../draft/use-draft';
import { sectionInfo } from '../sections';
import { conflictRows } from './state';

const actionButton = 'h-11 md:h-10';

export function ConflictDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const session = useDraftSession();
  const conflicts = useDraft((s) => s.conflicts);
  const pending = sections.flatMap((s) => (conflicts[s] ? [conflicts[s] as SectionConflict] : []));
  const [keep, setKeep] = useState<Record<string, 'theirs' | 'ours'>>({});

  const choice = (section: Section, pointer: string) => keep[`${section}:${pointer}`] ?? 'ours';
  const close = () => {
    setKeep({});
    onOpenChange(false);
  };
  const save = () => {
    for (const c of pending) {
      const rows = c.merged.conflicts.map((x) => [x.pointer, choice(c.section, x.pointer)] as const);
      session.resolveSection(c.section, Object.fromEntries(rows));
    }
    close();
  };
  const keepAllMine = () => {
    for (const c of pending) session.keepMine(c.section);
    close();
  };

  return (
    <Dialog open={open && pending.length > 0} onOpenChange={(o) => (o ? onOpenChange(true) : close())}>
      <DialogContent
        title="Changed here and elsewhere"
        description="Another tab or the CLI saved the same fields. Choose which value each keeps; your other changes are already merged."
        className="max-w-[560px]"
      >
        <div className="flex flex-col gap-4">
          {pending.map((c) => (
            <section key={c.section} className="flex flex-col gap-2.5">
              <h3 className="m-0 text-sm font-medium text-muted">{sectionInfo[c.section].label}</h3>
              {conflictRows(c.section, c.merged.conflicts).map((row) => {
                const name = `${c.section}:${row.pointer}`;
                const set = (value: 'theirs' | 'ours') => setKeep((k) => ({ ...k, [name]: value }));
                return (
                  <fieldset
                    key={row.pointer}
                    className="m-0 flex flex-col gap-2 rounded-control border border-line bg-surface p-3"
                  >
                    <legend className="float-left mb-1 w-full p-0 text-[15px] leading-snug">
                      <span className="font-medium">{row.label}</span> also changed elsewhere: theirs{' '}
                      <span className="font-mono text-sm break-all">{row.theirs}</span>, yours{' '}
                      <span className="font-mono text-sm break-all">{row.ours}</span>
                    </legend>
                    <div className="flex flex-wrap gap-x-5 gap-y-1">
                      {(['theirs', 'ours'] as const).map((value) => (
                        <label
                          key={value}
                          className="inline-flex min-h-11 cursor-pointer items-center gap-2 text-sm md:min-h-8"
                        >
                          <input
                            type="radio"
                            name={name}
                            value={value}
                            checked={choice(c.section, row.pointer) === value}
                            onChange={() => set(value)}
                            className="size-4 accent-ink"
                          />
                          {value === 'theirs' ? 'Keep theirs' : 'Keep mine'}
                        </label>
                      ))}
                    </div>
                  </fieldset>
                );
              })}
            </section>
          ))}
        </div>
        <div className="flex flex-wrap justify-end gap-2">
          <Button variant="secondary" className={actionButton} onClick={keepAllMine}>
            Keep all mine
          </Button>
          <Button variant="primary" className={actionButton} onClick={save}>
            Save these choices
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export function FileConflictDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const session = useDraftSession();
  const fileConflicts = useDraft((s) => s.fileConflicts);
  const conflict = Object.values(fileConflicts)[0];
  const theirs = useQuery({
    ...draftFileQuery(session.org, session.factory, conflict?.path ?? ''),
    enabled: conflict !== undefined && conflict.theirsSha !== null,
    staleTime: 0,
  });
  if (!conflict) return null;

  const theirsText = conflict.theirsSha === null ? null : theirs.data?.text;
  const takeTheirs = async () => {
    try {
      await navigator.clipboard.writeText(conflict.ours);
      toast.success('Your version is on the clipboard');
    } catch {
      toast.error('Your version could not be copied');
    }
    session.resolveFile(conflict.path, 'theirs', theirsText ?? '');
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={`${conflict.path} changed elsewhere`}
        description="Another tab or the CLI saved this file while you edited it."
        className="max-w-[880px]"
      >
        <div className="grid gap-3 md:grid-cols-2">
          {[
            ['Theirs', conflict.theirsSha === null ? 'The file was removed.' : (theirsText ?? 'Loading…')],
            ['Yours', conflict.ours],
          ].map(([label, text]) => (
            <figure key={label} className="m-0 flex min-w-0 flex-col gap-1.5">
              <figcaption className="text-sm font-medium">{label}</figcaption>
              <pre className="m-0 max-h-72 overflow-auto rounded-control border border-line bg-surface p-3 font-mono text-[13px] leading-relaxed whitespace-pre-wrap">
                {text}
              </pre>
            </figure>
          ))}
        </div>
        <div className="flex flex-wrap justify-end gap-2">
          <Button
            variant="secondary"
            className={actionButton}
            disabled={conflict.theirsSha !== null && theirsText === undefined}
            onClick={() => void takeTheirs()}
          >
            Use theirs
          </Button>
          <Button variant="primary" className={actionButton} onClick={() => session.resolveFile(conflict.path, 'mine')}>
            Overwrite with mine
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export function DiscardedDialog({ onLeave }: { onLeave: () => void }) {
  const session = useDraftSession();
  const discarded = useDraft((s) => s.discarded);
  // Closed, the status line keeps saying so, with Start a new draft.
  const [dismissed, setDismissed] = useState(false);
  if (!discarded && dismissed) setDismissed(false);
  return (
    <Dialog open={discarded && !dismissed} onOpenChange={(o) => setDismissed(!o)}>
      <DialogContent
        title="This draft was discarded in another tab or the CLI"
        description="Your changes that weren’t saved are still here. Start a new draft with them, or leave without them."
      >
        <div className="flex flex-wrap justify-end gap-2">
          <Button variant="secondary" className={actionButton} onClick={onLeave}>
            Leave
          </Button>
          <Button variant="primary" className={actionButton} onClick={() => session.restart()}>
            Start a new draft
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
