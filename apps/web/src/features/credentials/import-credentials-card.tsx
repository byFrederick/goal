// "Or import many" on the New credential page (canvas credential-new.jpg): paste or drop a .env or YAML
// text. A dry run answers what each name would do ("New", or "Already exists" with the connections that
// use it); Replace or Skip decides the existing ones; Import N writes them. The text lives in this
// component's state only: the mutations are not kept in the cache (gcTime 0) and nothing reaches the URL.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { importCredentials } from '../../api/credentials';
import { keys } from '../../api/queries';
import type { CredentialImport, CredentialImportResult } from '../../api/types';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { Field, Textarea } from '../../ui/field';
import { SegmentedControl } from '../../ui/segmented-control';
import { refusalText } from '../connections/words';
import { importCount, importFormat, usedByConnections } from './words';

type OnConflict = 'replace' | 'skip';

/** What the paste area shows while it is empty: the shape of a YAML import, with its values masked. */
const maskedSample =
  'credentials:\n  sentry-token: sntrys_••••••••\n  linear-key: lin_api_••••••••\n  cloudflare-token: cf_••••••••';

/** The import panel. `slug` is the organization's URL name; `focus` focuses the paste area (?panel=import). */
export function ImportCredentialsCard({ org, slug, focus }: { org: string; slug: string; focus: boolean }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const paste = useRef<HTMLTextAreaElement>(null);
  const [text, setText] = useState('');
  const [onConflict, setOnConflict] = useState<OnConflict>('skip');
  const [found, setFound] = useState<CredentialImportResult | null>(null);

  useEffect(() => {
    if (focus) paste.current?.focus();
  }, [focus]);

  const dryRun = useMutation({
    mutationFn: (pasted: string) =>
      importCredentials(org, { format: importFormat(pasted), text: pasted, dry_run: true }),
    gcTime: 0,
    onSuccess: (result) => setFound(result),
    onError: () => setFound(null),
  });
  const runDryRun = dryRun.mutate;

  // A dry run after the text settles; an empty paste clears the list. Nothing is stored by it.
  useEffect(() => {
    if (text.trim() === '') {
      setFound(null);
      return;
    }
    const timer = setTimeout(() => runDryRun(text), 300);
    return () => clearTimeout(timer);
  }, [text, runDryRun]);

  const run = useMutation({
    mutationFn: (body: CredentialImport) => importCredentials(org, body),
    gcTime: 0,
    onSuccess: async (result) => {
      await client.invalidateQueries({ queryKey: keys.credentials(org) });
      const written = result.credentials.filter((c) => c.outcome === 'created' || c.outcome === 'replaced').length;
      toast.success(`Imported ${written} ${written === 1 ? 'credential' : 'credentials'}`, {
        description: 'Imported credentials start with no factories. Choose who can use each one.',
      });
      setText('');
      setFound(null);
      void navigate({ to: '/o/$org/credentials', params: { org: slug } });
    },
  });

  const credentials = found?.credentials ?? [];
  const count = importCount(credentials, onConflict);
  const hasExisting = credentials.some((c) => c.exists);

  const read = async (file: File | undefined) => {
    if (file) setText(await file.text());
  };

  return (
    <section
      aria-labelledby="import-heading"
      className="flex min-w-0 flex-col gap-4 rounded-card border border-line bg-card p-5"
    >
      <div className="flex items-center gap-3">
        <h2 id="import-heading" className="m-0 text-heading">
          Or import many
        </h2>
        <span className="inline-flex h-6 items-center rounded-chip border border-line bg-panel px-2 text-meta font-control text-ink-soft">
          .env or YAML
        </span>
      </div>

      <label
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => {
          e.preventDefault();
          void read(e.dataTransfer.files[0]);
        }}
        className="flex h-11 cursor-pointer items-center justify-center gap-2 rounded-control border border-dashed border-rule text-secondary font-control text-ink-soft hover:border-muted"
      >
        <input
          type="file"
          accept=".env,.yaml,.yml,text/plain"
          className="sr-only"
          onChange={(e) => void read(e.target.files?.[0])}
        />
        Drop a file, or paste below
      </label>

      {/* Pasted values stay masked on screen: the browser shows each character as a dot. */}
      <Field label={<span className="sr-only">Paste .env or YAML</span>}>
        <Textarea
          ref={paste}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={maskedSample}
          rows={5}
          spellCheck={false}
          autoComplete="off"
          data-1p-ignore=""
          className="min-h-32 font-mono text-secondary [-webkit-text-security:disc]"
        />
      </Field>
      <p className="m-0 text-meta text-muted">Values stay masked. They are sealed when you import them.</p>

      {dryRun.isPending ? (
        <p role="status" aria-busy="true" className="m-0 text-secondary text-muted">
          Checking the names
        </p>
      ) : null}
      {dryRun.isError ? <Callout tone="rework">{refusalText(dryRun.error)}</Callout> : null}

      {found ? (
        <div className="flex flex-col">
          <p className="m-0 mb-1 text-secondary font-semibold text-ink">{credentials.length} found</p>
          <ul className="m-0 flex list-none flex-col p-0">
            {credentials.map((c) => (
              <li
                key={c.name}
                className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-2 gap-y-1 border-t border-line py-2 text-body"
              >
                <span className="min-w-0 truncate font-mono text-secondary">{c.name}</span>
                {c.exists ? <Chip tone="rework">Already exists</Chip> : <Chip tone="ok">New</Chip>}
                {c.exists ? (
                  <span className="col-span-full text-meta text-muted">
                    {usedByConnections(c.connections)}. Replace its value, or skip it.
                  </span>
                ) : null}
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {run.isError ? <Callout tone="rework">{refusalText(run.error)}</Callout> : null}

      <div className={cn('flex flex-wrap items-center gap-2', !hasExisting && 'justify-end')}>
        {hasExisting ? (
          <SegmentedControl<OnConflict>
            label="When a name exists"
            value={onConflict}
            onChange={setOnConflict}
            options={[
              { value: 'replace', label: 'Replace' },
              { value: 'skip', label: 'Skip' },
            ]}
          />
        ) : null}
        <button
          type="button"
          disabled={count === 0 || run.isPending}
          onClick={() => run.mutate({ format: importFormat(text), text, on_conflict: onConflict, dry_run: false })}
          className="ml-auto inline-flex h-11 items-center rounded-control border border-rule bg-card px-4 text-body font-control text-ink hover:border-muted disabled:opacity-60 md:h-10"
        >
          {run.isPending ? 'Importing…' : `Import ${count}`}
        </button>
      </div>
      <p className="m-0 text-secondary text-muted">
        Imported credentials start with no factories. You choose who can use each one next.
      </p>
    </section>
  );
}
