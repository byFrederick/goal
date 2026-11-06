// Import secrets (ADR 0037 decision 14, canvas docs/design/screens/connections/secrets-import.jpg): paste or
// drop a .env or YAML text. A dry run answers what each name would do ("New", or "Already exists" with where it
// is used); Replace or Skip decides the existing ones; Import N writes them. The text lives in this component's
// state only: the mutations are not kept in the cache (gcTime 0) and nothing reaches the URL.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { keys } from '../../api/queries';
import { importSecrets } from '../../api/secrets';
import type { SecretImport, SecretImportResult } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { plural } from '../../shared/format';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { Field, Textarea } from '../../ui/field';
import { SegmentedControl } from '../../ui/segmented-control';
import { fileNames, importCount, importFormat, refusalText, usedInText } from './words';

type OnConflict = 'replace' | 'skip';

/** What the paste area shows while it is empty: the shape of an import. */
const sample = 'SENTRY_TOKEN=sntrys_••••••••\nLINEAR_API_KEY=lin_api_••••••••\nNPM_TOKEN=npm_••••••••';

const card = 'flex min-w-0 flex-col rounded-card border border-line bg-card';
const cardHead = 'flex min-h-14 items-center justify-between gap-3 border-b border-line px-4 py-2 md:px-5';

export function ImportSecretsPage() {
  const org = useOrg();
  const client = useQueryClient();
  const navigate = useNavigate();
  const manage = useOrgHolds('secret:manage');
  const [text, setText] = useState('');
  const [onConflict, setOnConflict] = useState<OnConflict>('skip');
  const [found, setFound] = useState<SecretImportResult | null>(null);

  const dryRun = useMutation({
    mutationFn: (pasted: string) =>
      importSecrets(org.id, { format: importFormat(pasted), text: pasted, dry_run: true }),
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
    mutationFn: (body: SecretImport) => importSecrets(org.id, body),
    gcTime: 0,
    onSuccess: async (result) => {
      await client.invalidateQueries({ queryKey: keys.secrets(org.id) });
      const written = result.secrets.filter((s) => s.outcome === 'created' || s.outcome === 'replaced').length;
      toast.success(`Imported ${plural(written, 'secret')}`, {
        description: 'Imported secrets are open to no factory until you choose it on each one.',
      });
      setText('');
      setFound(null);
      void navigate({ to: '/o/$org/secrets', params: { org: org.slug } });
    },
  });

  const secrets = found?.secrets ?? [];
  const count = importCount(secrets, onConflict);
  const existing = secrets.filter((s) => s.exists).length;
  const spelled = fileNames(text);

  const read = async (file: File | undefined) => {
    if (file) setText(await file.text());
  };

  return (
    <>
      <TopBarSlot
        crumb="Import"
        actions={
          <Button asChild variant="white" className="h-11 md:h-8.5">
            <Link to="/o/$org/secrets" params={{ org: org.slug }}>
              Cancel
            </Link>
          </Button>
        }
      />
      <div className="flex min-w-0 flex-col gap-1 md:gap-2">
        <h1 className="m-0 text-page-title tracking-display">Import secrets</h1>
        <p className="m-0 text-body text-ink-soft">
          Paste a .env file or YAML with a{' '}
          <code className="rounded-chip bg-hover px-1 font-mono text-meta">secrets:</code> map. Nothing is saved until
          you import.
        </p>
      </div>

      {!manage ? (
        <Callout tone="note">Owners and admins of {org.name} import secrets.</Callout>
      ) : (
        <div className="@container">
          <div className="grid items-start gap-5 @[900px]:grid-cols-[minmax(0,1fr)_minmax(0,448px)]">
            <div className="flex min-w-0 flex-col gap-5">
              <section aria-labelledby="paste-heading" className={card}>
                <div className={cardHead}>
                  <h2 id="paste-heading" className="m-0 text-heading">
                    Paste or drop a file
                  </h2>
                  <label className="inline-flex h-11 cursor-pointer items-center rounded-control border border-rule bg-card px-3 text-body font-control text-ink transition-colors duration-(--duration-quick) hover:border-muted focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-run md:h-8.5">
                    <input
                      type="file"
                      accept=".env,.yaml,.yml,text/plain"
                      className="sr-only"
                      onChange={(e) => void read(e.target.files?.[0])}
                    />
                    Choose a file
                  </label>
                </div>
                <div className="p-4 md:p-5">
                  {/* Pasted values stay masked on screen: the browser shows each character as a dot. */}
                  <Field label={<span className="sr-only">Paste .env or YAML</span>}>
                    <Textarea
                      value={text}
                      onChange={(e) => setText(e.target.value)}
                      onDragOver={(e) => e.preventDefault()}
                      onDrop={(e) => {
                        e.preventDefault();
                        void read(e.dataTransfer.files[0]);
                      }}
                      placeholder={sample}
                      rows={7}
                      spellCheck={false}
                      autoComplete="off"
                      data-1p-ignore=""
                      className="min-h-40 font-mono text-secondary [-webkit-text-security:disc]"
                    />
                  </Field>
                </div>
              </section>

              {dryRun.isPending || dryRun.isError || found ? (
                <section aria-labelledby="happens-heading" className={card}>
                  <div className={cardHead}>
                    <h2 id="happens-heading" className="m-0 text-heading">
                      What happens
                    </h2>
                    {found ? (
                      <span className="text-secondary text-muted">
                        {secrets.length} found, {secrets.length - existing} new, {existing} already{' '}
                        {existing === 1 ? 'exists' : 'exist'}
                      </span>
                    ) : null}
                  </div>
                  {dryRun.isPending && !found ? (
                    <p role="status" aria-busy="true" className="m-0 px-4 py-4 text-secondary text-muted md:px-5">
                      Checking the names
                    </p>
                  ) : null}
                  {dryRun.isError ? (
                    <div role="alert" className="p-4 md:px-5">
                      <Callout tone="rework">{refusalText(dryRun.error)}</Callout>
                    </div>
                  ) : null}
                  {found ? (
                    <ul className="m-0 flex list-none flex-col p-0">
                      {secrets.map((s) => (
                        <li
                          key={s.name}
                          className="grid min-h-14 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 border-t border-line px-4 py-3 first:border-t-0 md:px-5"
                        >
                          <span className="flex min-w-0 flex-col">
                            <span className="break-all font-mono text-body font-medium">{s.name}</span>
                            {spelled.get(s.name) && spelled.get(s.name) !== s.name ? (
                              <span className="break-all font-mono text-meta text-muted">{spelled.get(s.name)}</span>
                            ) : null}
                          </span>
                          {s.exists ? (
                            <Chip tone="rework">Already exists</Chip>
                          ) : (
                            <Chip tone="ok" dot>
                              New
                            </Chip>
                          )}
                          {s.exists ? (
                            <span className="col-span-full text-meta text-muted">
                              {usedInText(s.uses.length)}.{' '}
                              {onConflict === 'replace' ? 'Its value is replaced.' : 'It is skipped.'}
                            </span>
                          ) : null}
                        </li>
                      ))}
                    </ul>
                  ) : null}
                </section>
              ) : null}
            </div>

            <div className={`${card} gap-4 p-4 md:p-5`}>
              <p className="m-0 text-secondary text-ink-soft">
                Names become lowercase with dashes: SENTRY_TOKEN becomes sentry-token. A replaced value applies to runs
                that start afterwards. Imported secrets are open to no factory until you choose it on each one.
              </p>
              {existing > 0 ? (
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="text-secondary text-ink">When a name already exists</span>
                  <SegmentedControl<OnConflict>
                    label="When a name already exists"
                    value={onConflict}
                    onChange={setOnConflict}
                    options={[
                      { value: 'replace', label: 'Replace' },
                      { value: 'skip', label: 'Skip' },
                    ]}
                  />
                </div>
              ) : null}
              {run.isError ? (
                <div role="alert">
                  <Callout tone="rework">{refusalText(run.error)}</Callout>
                </div>
              ) : null}
              <div className="flex flex-wrap justify-end gap-2">
                <Button asChild variant="white" className="h-11 md:h-10">
                  <Link to="/o/$org/secrets" params={{ org: org.slug }}>
                    Cancel
                  </Link>
                </Button>
                <Button
                  variant="action"
                  className="h-11 md:h-10"
                  disabled={count === 0}
                  busy={run.isPending}
                  busyLabel="Importing…"
                  onClick={() =>
                    run.mutate({ format: importFormat(text), text, on_conflict: onConflict, dry_run: false })
                  }
                >
                  {`Import ${count}`}
                </Button>
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
