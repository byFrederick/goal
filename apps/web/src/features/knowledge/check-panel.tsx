// The Check panel of Add a source (ADR 0038 decision 12): asks goald to read the source with the
// settings as typed, before anything is saved, and answers what the page's form cannot know: whether
// the source can be read, how many files and bytes it has against the limits, what its factories'
// drafts would say about it, and the line agents will see. Check again re-reads it. The rows ease in
// when the answer arrives (MOTION.md, A status changes in place); the button says it works.
import { useMutation } from '@tanstack/react-query';
import { Check, TriangleAlert } from 'lucide-react';
import type { ReactNode } from 'react';
import { checkKnowledgeSource } from '../../api/knowledge';
import type { KnowledgeCheck, KnowledgeCheckInput } from '../../api/types';
import { plural } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Skeleton } from '../../ui/skeleton';
import { formatBytes, refusalText } from './words';

export function CheckPanel({ input }: { input: KnowledgeCheckInput | null }) {
  const org = useOrg();
  const check = useMutation({
    mutationFn: (body: KnowledgeCheckInput) => checkKnowledgeSource(org.id, body),
  });
  const result = check.data;

  const showFiles = check.isPending || Boolean(result?.access.ok && result.first_files.length > 0);
  return (
    <>
      <section aria-labelledby="check-h" className="flex min-w-0 flex-col rounded-card border border-line bg-card">
        <div className="flex items-center justify-between gap-3 px-5 py-3">
          <h2 id="check-h" className="m-0 text-heading">
            Check
          </h2>
          <Button
            variant="secondary"
            size="sm"
            disabled={!input}
            busy={check.isPending}
            busyLabel="Checking…"
            onClick={() => input && check.mutate(input)}
          >
            {result ? 'Check again' : 'Check'}
          </Button>
        </div>
        <div aria-live="polite" className="border-t border-line">
          {check.isError ? (
            <div className="p-4">
              <Callout tone="rework">{refusalText(check.error)}</Callout>
            </div>
          ) : result ? (
            <CheckRows result={result} />
          ) : (
            <p className="m-0 px-5 py-4 text-body text-muted">
              {input ? 'Not checked yet.' : 'Fill in the source’s settings, then check them.'}
            </p>
          )}
        </div>
      </section>
      {showFiles ? <FirstFiles result={check.isPending ? undefined : result} /> : null}
    </>
  );
}

/** The paths of the first files the listing yields, under the Check; its skeleton shows while the check runs. */
function FirstFiles({ result }: { result: KnowledgeCheck | undefined }) {
  const paths = result?.first_files ?? [];
  const more = result ? result.files - paths.length : 0;
  return (
    <section aria-labelledby="first-files-h" className="flex min-w-0 flex-col rounded-card border border-line bg-card">
      <h2 id="first-files-h" className="m-0 px-5 py-3 text-heading">
        First files
      </h2>
      <Skeleton
        ready={Boolean(result)}
        className="border-t border-line"
        placeholder={
          <div className="flex flex-col gap-2 px-5 py-4">
            <div className="h-4 w-2/3 rounded-control bg-hover" />
            <div className="h-4 w-1/2 rounded-control bg-hover" />
            <div className="h-4 w-3/5 rounded-control bg-hover" />
          </div>
        }
      >
        <div className="flex animate-row-in flex-col gap-2 px-5 py-4">
          <ul className="m-0 flex list-none flex-col gap-1 p-0 font-mono text-secondary">
            {paths.map((path) => (
              <li key={path} className="break-all text-ink">
                {path}
              </li>
            ))}
          </ul>
          {more > 0 ? <p className="m-0 text-secondary text-muted">and {more.toLocaleString('en-US')} more</p> : null}
        </div>
      </Skeleton>
    </section>
  );
}

function CheckRows({ result }: { result: KnowledgeCheck }) {
  const { access, limits } = result;
  const size = result.bytes === undefined ? undefined : formatBytes(result.bytes);
  const overFiles = result.over_limit.includes('files');
  const overBytes = result.over_limit.includes('bytes');

  return (
    <>
      <ul className="m-0 flex list-none flex-col p-0">
        <Row ok={access.ok} title={access.ok ? 'Access works' : 'Couldn’t read the source'}>
          {access.ok ? undefined : access.error}
        </Row>
        {access.ok ? (
          <>
            <Row ok={!overFiles && !overBytes} title={`${plural(result.files, 'file')}${size ? `, ${size}` : ''}`}>
              {`Limits: ${limits.files.toLocaleString('en-US')} files, ${formatBytes(limits.bytes)}, ${formatBytes(limits.file_bytes)} a file.`}
            </Row>
            {overFiles ? (
              <Row ok={false} title="Over the file limit">
                A sync would fail on it.
              </Row>
            ) : null}
            {overBytes ? (
              <Row ok={false} title="Over the size limit">
                A sync would fail on it.
              </Row>
            ) : null}
          </>
        ) : null}
        {result.warnings.map((w) => (
          <Row
            key={`${w.code}-${w.factory ?? ''}`}
            ok={false}
            title={w.factory ? `${w.factory}: ${w.message}` : w.message}
          />
        ))}
      </ul>
      <div className="flex flex-col gap-2 border-t border-line px-5 py-4">
        <h3 className="m-0 text-body font-control">What agents will see</h3>
        <p className="m-0 rounded-control bg-panel p-3 font-mono text-secondary break-words">{result.line}</p>
      </div>
    </>
  );
}

function Row({ ok, title, children }: { ok: boolean; title: string; children?: ReactNode }) {
  const Icon = ok ? Check : TriangleAlert;
  return (
    <li className="flex animate-row-in items-start gap-3 border-b border-line px-5 py-3 last:border-b-0">
      <Icon
        size={16}
        strokeWidth={1.75}
        aria-hidden="true"
        className={ok ? 'mt-1 shrink-0 text-ok-ink' : 'mt-1 shrink-0 text-rework-ink'}
      />
      <span className="flex min-w-0 flex-col">
        <span className="text-body break-words text-ink">{title}</span>
        {children ? <span className="text-secondary break-words text-muted">{children}</span> : null}
      </span>
    </li>
  );
}
