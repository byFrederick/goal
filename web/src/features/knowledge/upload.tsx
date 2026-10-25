// Uploading files to an upload source (spec 4 §8.2, §10): a drop zone that is also a button
// (Enter or Space chooses files; dragging is never the only way, WCAG 2.5.7), and each file's own
// row with its progress and, when refused, why. Files go one request each, so one refused file
// (too large, not text) never holds back the others; goald checks a request's files before it
// stores any. A file goald won't take by its type is refused here before it is sent.
import { useQueryClient } from '@tanstack/react-query';
import { Check, LoaderCircle, TriangleAlert, Upload, X } from 'lucide-react';
import { type DragEvent, useId, useRef, useState } from 'react';
import { uploadKnowledgeDocuments } from '../../api/knowledge';
import { keys } from '../../api/queries';
import type { KnowledgeSource } from '../../api/types';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { Dialog, DialogContent } from '../../ui/dialog';
import { formatBytes, refusalText, refusedFile, uploadTypes } from './words';

export interface UploadRow {
  key: string;
  file: File;
  state: 'ready' | 'refused' | 'uploading' | 'uploaded';
  /** Why it was refused, here or by goald. */
  reason?: string;
}

/** The files chosen for an upload source and how each one's upload went. */
export function useUploader(org: string) {
  const client = useQueryClient();
  const [rows, setRows] = useState<UploadRow[]>([]);
  const made = useRef(0);
  const set = (key: string, patch: Partial<UploadRow>) =>
    setRows((all) => all.map((r) => (r.key === key ? { ...r, ...patch } : r)));

  return {
    rows,
    busy: rows.some((r) => r.state === 'uploading'),
    /** Adds files, refusing those goald won't take by type; answers the rows made. */
    add(files: readonly File[]): UploadRow[] {
      const added = files.map((file): UploadRow => {
        const reason = refusedFile(file.name);
        made.current += 1;
        return {
          key: `upload-${made.current}`,
          file,
          state: reason ? 'refused' : 'ready',
          ...(reason ? { reason } : {}),
        };
      });
      setRows((all) => [...all, ...added]);
      return added;
    },
    remove: (key: string) => setRows((all) => all.filter((r) => r.key !== key)),
    /** Forgets the rows whose upload is over. */
    clear: () => setRows((all) => all.filter((r) => r.state === 'ready' || r.state === 'uploading')),
    /** Uploads the ready ones of `chosen` to the source, one after the other; answers how many went. */
    async upload(source: string, chosen: readonly UploadRow[]): Promise<{ uploaded: number; refused: number }> {
      let uploaded = 0;
      let refused = 0;
      for (const r of chosen.filter((x) => x.state === 'ready')) {
        set(r.key, { state: 'uploading' });
        try {
          await uploadKnowledgeDocuments(org, source, [r.file]);
          set(r.key, { state: 'uploaded' });
          uploaded += 1;
        } catch (err) {
          set(r.key, { state: 'refused', reason: refusalText(err) });
          refused += 1;
        }
      }
      if (uploaded > 0) await client.invalidateQueries({ queryKey: keys.knowledge(org) });
      return { uploaded, refused };
    },
  };
}

export const uploadLimits =
  'Text, Markdown, HTML, JSON, YAML, each up to goald’s upload limit (25 MiB by default). PDF and Office files come later.';

/** Drop files on it, or press it (Enter, Space, a tap) to choose them. */
export function DropZone({ onFiles, disabled = false }: { onFiles: (files: File[]) => void; disabled?: boolean }) {
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  const hint = useId();
  const drop = (e: DragEvent) => {
    e.preventDefault();
    setOver(false);
    if (!disabled && e.dataTransfer.files.length > 0) onFiles([...e.dataTransfer.files]);
  };
  return (
    <>
      <button
        type="button"
        disabled={disabled}
        aria-describedby={hint}
        onClick={() => input.current?.click()}
        onDragOver={(e) => {
          e.preventDefault();
          if (!disabled) setOver(true);
        }}
        onDragLeave={() => setOver(false)}
        onDrop={drop}
        className={cn(
          'flex min-h-32 w-full cursor-pointer flex-col items-center justify-center gap-2 rounded-control border border-dashed px-4 py-6 text-center text-ink transition-colors duration-200',
          'disabled:cursor-not-allowed disabled:opacity-55',
          over ? 'border-run bg-surface' : 'border-rule-strong bg-transparent hover:bg-surface',
        )}
      >
        <Upload size={18} strokeWidth={1.5} aria-hidden="true" />
        <span className="text-[15px]">
          <strong className="font-medium">Drop files here</strong>
          <span className="hidden md:inline"> or press Enter to choose them</span>
          <span className="md:hidden"> or tap to choose them</span>
        </span>
        <span id={hint} className="max-w-[60ch] text-[13px] leading-snug text-muted">
          {uploadLimits}
        </span>
      </button>
      <input
        ref={input}
        type="file"
        multiple
        accept={uploadTypes.join(',')}
        hidden
        onChange={(e) => {
          const files = [...(e.target.files ?? [])];
          e.target.value = '';
          if (files.length > 0) onFiles(files);
        }}
      />
    </>
  );
}

const rowText: Record<UploadRow['state'], string> = {
  ready: 'Ready to upload',
  uploading: 'Uploading…',
  uploaded: 'Uploaded, indexing starts at once',
  refused: 'Refused',
};

/** Each chosen file: its name, size and how its upload went; Remove before it is sent. */
export function UploadRows({
  rows,
  onRemove,
}: {
  rows: readonly UploadRow[];
  onRemove?: ((key: string) => void) | undefined;
}) {
  if (rows.length === 0) return null;
  const count = (state: UploadRow['state']) => rows.filter((r) => r.state === state).length;
  const summary = [
    count('uploading') > 0 ? `${count('uploading')} uploading` : '',
    count('uploaded') > 0 ? `${count('uploaded')} uploaded` : '',
    count('refused') > 0 ? `${count('refused')} refused` : '',
  ].filter(Boolean);
  return (
    <>
      <p role="status" className="sr-only">
        {summary.join(', ')}
      </p>
      <ul aria-label="Files" className="m-0 flex list-none flex-col p-0">
        {rows.map((r) => {
          const Icon =
            r.state === 'uploading'
              ? LoaderCircle
              : r.state === 'uploaded'
                ? Check
                : r.state === 'refused'
                  ? TriangleAlert
                  : Upload;
          return (
            <li
              key={r.key}
              className="flex items-start justify-between gap-3 border-t border-line py-2.5 first:border-t-0"
            >
              <span className="flex min-w-0 flex-col gap-0.5 text-sm">
                <span className="break-all font-medium" translate="no">
                  {r.file.name} <span className="font-normal text-muted">{formatBytes(r.file.size)}</span>
                </span>
                <span
                  className={cn(
                    'inline-flex items-start gap-1.5',
                    r.state === 'refused' ? 'text-rework-ink' : r.state === 'uploaded' ? 'text-ok-ink' : 'text-muted',
                  )}
                >
                  <Icon
                    size={15}
                    strokeWidth={1.75}
                    aria-hidden="true"
                    className={cn('mt-[2px] shrink-0', r.state === 'uploading' && 'motion-safe:animate-spin')}
                  />
                  <span>{r.state === 'refused' && r.reason ? `Refused: ${r.reason}` : rowText[r.state]}</span>
                </span>
              </span>
              {onRemove && (r.state === 'ready' || r.state === 'refused') ? (
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-11 md:size-9"
                  aria-label={`Remove ${r.file.name}`}
                  onClick={() => onRemove(r.key)}
                >
                  <X size={16} strokeWidth={1.5} aria-hidden="true" />
                </Button>
              ) : null}
            </li>
          );
        })}
      </ul>
    </>
  );
}

/**
 * Upload… on a source's row: the drop zone in a dialog; every file chosen goes at once, and the
 * dialog stays open showing how each went until it is closed.
 */
export function UploadDialog({ source, onClose }: { source: KnowledgeSource | null; onClose: () => void }) {
  const org = useOrg();
  const uploader = useUploader(org.id);
  return (
    <Dialog
      open={source !== null}
      onOpenChange={(open) => {
        if (!open && !uploader.busy) {
          uploader.clear();
          onClose();
        }
      }}
    >
      {source ? (
        <DialogContent title={`Upload to ${source.title}`} className="max-w-[560px]">
          <DropZone onFiles={(files) => void uploader.upload(source.name, uploader.add(files))} />
          <UploadRows rows={uploader.rows} />
        </DialogContent>
      ) : null}
    </Dialog>
  );
}
