// Kept tests in the Publish card (ADR 0039 decision 12, docs/design/screens/build/definition-publish): the kept
// tests of the stations the draft reaches, each with its result as it arrives. The card starts them once when it
// opens (startDraftKeptTests); the list then reads them (getDraftKeptTests) while any is still trying. A failing
// test warns and never blocks: the Publish button does not read this list.
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { startDraftKeptTests } from '../../../api/client';
import { draftKeptTestsQuery } from '../../../api/queries';
import type { DraftKeptTest } from '../../../api/types';
import { humanize } from '../../../shared/format';
import { Chip, type ChipTone } from '../../../ui/chip';
import { Skeleton } from '../../../ui/skeleton';
import { InfoTip } from '../../jonah/info-tip';
import { useEditorFacts } from './use-review';

const pollMs = 3000;

const labels: Record<string, { label: string; tone: ChipTone }> = {
  waiting: { label: 'Waiting', tone: 'plain' },
  running: { label: 'Running', tone: 'run' },
  passed: { label: 'Passed', tone: 'ok' },
  failed: { label: 'Failed', tone: 'rework' },
  error: { label: 'Not tried', tone: 'plain' },
};

/** A test's result in words: not started, running, passed, failed, or could not be tried. */
export function resultOf(t: DraftKeptTest): { label: string; tone: ChipTone } {
  return labels[t.result ?? 'waiting'] ?? { label: 'Waiting', tone: 'plain' };
}

/** Why a failing or untried test is flagged: the grader's reason, or why it could not be tried. */
export function warningOf(t: DraftKeptTest): string | undefined {
  if (t.result === 'failed') return t.reason ?? 'It did not pass.';
  if (t.result === 'error') return t.error ?? 'It could not be tried.';
  return undefined;
}

const block = 'rounded-control bg-hover';

export function KeptTestsList() {
  const { org, factory, hasDraft } = useEditorFacts();
  const client = useQueryClient();
  const key = draftKeptTestsQuery(org, factory).queryKey;
  const [started, setStarted] = useState(false);
  const [startFailed, setStartFailed] = useState(false);

  // One start per opening of the card; the polls that follow only read.
  // biome-ignore lint/correctness/useExhaustiveDependencies: started once per draft, not per cache change
  useEffect(() => {
    if (!hasDraft) return;
    let live = true;
    startDraftKeptTests(org, factory)
      .then((answer) => {
        if (!live) return;
        client.setQueryData(key, answer);
        setStarted(true);
      })
      .catch(() => live && setStartFailed(true));
    return () => {
      live = false;
    };
  }, [org, factory, hasDraft]);

  const query = useQuery({
    ...draftKeptTestsQuery(org, factory),
    enabled: hasDraft && started,
    // The start answer just seeded the cache; the first read comes one poll later.
    staleTime: pollMs,
    refetchInterval: (q) => (q.state.data?.state === 'trying' ? pollMs : false),
  });
  const data = query.data;

  if (startFailed && !data) return null;
  if (data && data.tests.length === 0) return null;

  return (
    <section aria-labelledby="kept-tests-h" className="flex flex-col gap-2">
      <div className="flex items-center gap-1">
        <h3 id="kept-tests-h" className="m-0 text-body font-medium">
          Kept tests
        </h3>
        <InfoTip label="How kept tests work at publish">
          Goal reruns the past items you kept as tests at the stations this change reaches. A failure warns you and
          never stops the publish.
        </InfoTip>
      </div>
      <Skeleton
        ready={Boolean(data)}
        placeholder={
          <div className="flex flex-col gap-2">
            <div className={`${block} h-9`} />
            <div className={`${block} h-9`} />
          </div>
        }
      >
        <ul className="m-0 flex list-none flex-col p-0">
          {(data?.tests ?? []).map((t) => {
            const result = resultOf(t);
            const warning = warningOf(t);
            return (
              <li
                key={t.test.id}
                className="flex min-w-0 flex-col gap-1 border-t border-line py-2 first:border-t-0 max-md:min-h-11"
              >
                <div className="flex min-w-0 items-center gap-2">
                  <span className="min-w-0 flex-1 break-words text-body">
                    <span className="mr-2 text-muted tabular-nums">#{t.test.item.number}</span>
                    {t.test.item.title}
                    <span className="ml-2 text-secondary text-muted">{humanize(t.test.station)}</span>
                  </span>
                  <Chip dot tone={result.tone}>
                    {result.label}
                  </Chip>
                </div>
                {warning ? <p className="m-0 text-secondary text-rework-ink break-words">{warning}</p> : null}
              </li>
            );
          })}
        </ul>
      </Skeleton>
    </section>
  );
}
