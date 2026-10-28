// The "Ask the factory to change its definition" bar (screens build/definition, ADR 0021): a request in
// words that goald runs in a box and writes into the draft, reviewed like any change. The bar shows the
// request ?request= names, else the newest one: its status while it runs, its summary when it is done and
// its error when it failed. The input and Ask are disabled while a request is queued or running (goald
// refuses another one). Nothing polls: a request's status moves on the stream's definition events, a
// resync or a reload (frontend.md, "Asked definition changes").
import { useQuery } from '@tanstack/react-query';
import { useSearch } from '@tanstack/react-router';
import { Sparkles } from 'lucide-react';
import { type FormEvent, useId, useState } from 'react';
import { draftRequestQuery, draftRequestsQuery } from '../../../api/queries';
import type { DraftRequest } from '../../../api/types';
import { useFactory } from '../../../shared/factory';
import { useOrg } from '../../../shared/org';
import { Button } from '../../../ui/button';
import { Callout } from '../../../ui/callout';
import { Chip } from '../../../ui/chip';
import { askFailure, isOpenRequest, useAskFactory } from '../draft-requests';

/** What the request is doing, with its summary once it is done or its error once it failed. */
function RequestStatus({ request }: { request: DraftRequest }) {
  if (request.status === 'failed') {
    return <Callout tone="rework">{request.error || 'The request failed.'}</Callout>;
  }
  if (request.status === 'done') {
    return (
      <p className="m-0 flex flex-wrap items-center gap-2 text-secondary text-ink-soft">
        <Chip tone="ok" dot>
          Done
        </Chip>
        {request.summary}
      </p>
    );
  }
  const queued = request.status === 'queued';
  return (
    <p className="m-0 flex flex-wrap items-center gap-2 text-secondary text-ink-soft">
      <Chip tone={queued ? 'quiet' : 'run'} dot>
        {queued ? 'Queued' : 'Running'}
      </Chip>
      {queued ? 'It runs when a box is free.' : 'It writes its change into the draft when it is done.'}
    </p>
  );
}

export function AskBar() {
  const org = useOrg();
  const factory = useFactory();
  const { request: requestId } = useSearch({ strict: false });
  const inputId = useId();
  const statusId = useId();
  const [text, setText] = useState('');

  const list = useQuery(draftRequestsQuery(org.id, factory.id));
  const named = useQuery({
    ...draftRequestQuery(org.id, factory.id, requestId ?? ''),
    enabled: requestId !== undefined,
  });
  const ask = useAskFactory();

  const newest = list.data?.requests[0];
  const shown = requestId === undefined ? newest : named.data;
  const busy = isOpenRequest(newest) || ask.isPending;
  const loading = list.isPending || (requestId !== undefined && named.isPending);
  const readFailed = list.isError || (requestId !== undefined && named.isError);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = text.trim();
    if (trimmed === '' || busy) return;
    ask.mutate({ text: trimmed }, { onSuccess: () => setText('') });
  }

  return (
    <div className="flex min-w-0 flex-col gap-2" aria-busy={loading || undefined}>
      <form
        onSubmit={submit}
        className="flex min-w-0 flex-wrap items-center gap-2 rounded-card border border-rule bg-shell py-1 pr-1 pl-3 focus-within:outline-2 focus-within:outline-run"
      >
        <Sparkles size={16} strokeWidth={1.75} aria-hidden="true" className="shrink-0 text-run" />
        <label htmlFor={inputId} className="sr-only">
          Ask the factory to change its definition
        </label>
        <input
          id={inputId}
          type="text"
          value={text}
          onChange={(event) => setText(event.target.value)}
          disabled={busy || loading}
          aria-describedby={statusId}
          placeholder="Ask the factory to change it, for example: give Review a 4th reviewer"
          className="min-w-0 flex-1 basis-64 bg-transparent text-body text-ink outline-none placeholder:text-muted disabled:cursor-not-allowed disabled:opacity-60"
        />
        <span className="shrink-0 text-meta whitespace-nowrap text-muted">
          Becomes an item, reviewed like any change
        </span>
        <Button type="submit" size="sm" variant="primary" disabled={busy || loading || text.trim() === ''}>
          Ask
        </Button>
      </form>
      <div id={statusId} role="status" aria-live="polite" className="flex min-w-0 flex-col gap-2">
        {loading ? <span className="sr-only">Loading</span> : null}
        {readFailed ? (
          <Callout tone="rework">
            The factory&rsquo;s requests could not be read. Check your connection and try again.
          </Callout>
        ) : null}
        {shown ? (
          <div className="flex min-w-0 flex-col gap-1">
            <p className="m-0 text-body break-words text-ink">{shown.text}</p>
            <RequestStatus request={shown} />
          </div>
        ) : null}
        {ask.isError ? <Callout tone="rework">{askFailure(ask.error)}</Callout> : null}
      </div>
    </div>
  );
}
