// New file… and Rename… (a name in the group's folder) and Delete… for the YAML view's files.
import { type FormEvent, useRef, useState } from 'react';
import { Button } from '../../../ui/button';
import { ConfirmDialog } from '../../../ui/confirm-dialog';
import { Dialog, DialogClose, DialogContent } from '../../../ui/dialog';
import { Field, Input } from '../../../ui/field';
import { fileNameProblem } from './files';

export type FileDialog =
  | { kind: 'new'; folder: string }
  | { kind: 'rename'; path: string }
  | { kind: 'delete'; path: string }
  | null;

const folderOf = (path: string) => path.split('/').slice(0, -1).join('/');
const nameOf = (path: string) => path.split('/').at(-1) ?? path;
const join = (folder: string, name: string) => (folder ? `${folder}/${name}` : name);

interface FileDialogsProps {
  dialog: FileDialog;
  onClose: () => void;
  /** The draft's paths: a name already taken is refused here. */
  taken: ReadonlySet<string>;
  onCreate: (path: string) => void;
  onRename: (from: string, to: string) => Promise<void>;
  onDelete: (path: string) => Promise<void>;
}

export function FileDialogs({ dialog, onClose, taken, onCreate, onRename, onDelete }: FileDialogsProps) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | undefined>();
  const run = async (work: () => Promise<void>) => {
    setPending(true);
    setError(undefined);
    try {
      await work();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'goald refused it');
    } finally {
      setPending(false);
    }
  };

  if (dialog?.kind === 'delete') {
    return (
      <ConfirmDialog
        open
        onOpenChange={(open) => !open && onClose()}
        title={`Delete ${dialog.path}?`}
        description="It leaves the draft. The running version keeps it until you publish."
        confirmLabel="Delete file"
        tone="danger"
        pending={pending}
        onConfirm={() => void run(() => onDelete(dialog.path))}
      >
        {error ? <p className="m-0 text-sm text-rework-ink">{error}</p> : null}
      </ConfirmDialog>
    );
  }
  if (dialog?.kind === 'new' || dialog?.kind === 'rename') {
    const folder = dialog.kind === 'new' ? dialog.folder : folderOf(dialog.path);
    return (
      <NameDialog
        key={dialog.kind === 'new' ? `new:${folder}` : dialog.path}
        title={dialog.kind === 'new' ? `New file in ${folder || 'the definition'}` : `Rename ${dialog.path}`}
        action={dialog.kind === 'new' ? 'Create file' : 'Rename file'}
        initial={dialog.kind === 'rename' ? nameOf(dialog.path) : ''}
        folder={folder}
        taken={taken}
        pending={pending}
        error={error}
        onClose={onClose}
        onSubmit={(name) => {
          const path = join(folder, name);
          if (dialog.kind === 'new') {
            onCreate(path);
            onClose();
          } else void run(() => onRename(dialog.path, path));
        }}
      />
    );
  }
  return null;
}

function NameDialog({
  title,
  action,
  initial,
  folder,
  taken,
  pending,
  error,
  onClose,
  onSubmit,
}: {
  title: string;
  action: string;
  initial: string;
  folder: string;
  taken: ReadonlySet<string>;
  pending: boolean;
  error: string | undefined;
  onClose: () => void;
  onSubmit: (name: string) => void;
}) {
  const [name, setName] = useState(initial);
  const [tried, setTried] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const problem =
    fileNameProblem(name) ?? (taken.has(join(folder, name)) ? 'A file with this name is already there.' : undefined);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setTried(true);
    if (!problem && !pending) onSubmit(name);
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent title={title} initialFocus={input}>
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          <Field label="File name" error={tried ? problem : error} help={folder ? `In ${folder}/` : undefined}>
            <Input
              ref={input}
              value={name}
              onChange={(e) => setName(e.target.value.trim())}
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              className="font-mono"
            />
          </Field>
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary" size="lg" className="md:h-10 md:text-sm">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" variant="primary" size="lg" className="md:h-10 md:text-sm" disabled={pending}>
              {action}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
