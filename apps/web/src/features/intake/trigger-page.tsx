// One intake trigger (ADR 0026; the picture is docs/design/screens/intake/trigger.jpg): its title with
// the on/off switch, this week's results, the When, Only if, Create and Limits blocks, its recent events
// and its YAML in the definition. Save changes writes spec.board.triggers through the draft files API and
// publishes a new version (trigger-save.ts). A name the definition does not have opens a blank trigger
// when the URL carries the event the New trigger picker chose (?on=<event key>, with &check=<agent> for a check). A trigger with `check` shows
// the check layout (intake/trigger-check.jpg): the Check block, its log of checks and Try a check.
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useParams, useSearch } from '@tanstack/react-router';
import { Check, MoreHorizontal } from 'lucide-react';
import { useState } from 'react';
import { ApiError } from '../../api/client';
import { definitionFile } from '../../api/intake';
import {
  clearStateMutation,
  definitionTextQuery,
  defKeys,
  draftQuery,
  factoryQuery,
  intakeKeys,
  intakeTriggerQuery,
  triggerChecksQuery,
} from '../../api/queries';
import type { DefinitionProblem, Factory, IntakeTriggerDetail } from '../../api/types';
import { TriggerCheckSkeleton, TriggerSkeleton } from '../../app/route-skeletons';
import { TopBarSlot } from '../../app/top-bar';
import type { Trigger } from '../../shared/definition/model';
import { factoryTitle, useFactory } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { useEdits } from '../../shared/use-edits';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { EmptyState } from '../../ui/empty-state';
import { NotAvailable } from '../../ui/not-available';
import { Switch } from '../../ui/switch';
import { InfoTip } from '../jonah/info-tip';
import { HighlightedYaml } from './template-text';
import { RecentChecks, TodaySummary } from './trigger-check-log';
import { RecentEvents, WeekSummary } from './trigger-events';
import { type BlockProblems, TriggerForm } from './trigger-form';
import { saveTrigger } from './trigger-save';
import { TryCheck, TryWithPastEvent } from './trigger-try';
import { cronWords } from './trigger-when';
import {
  type BoardChoices,
  type KeyLines,
  keyLines,
  readBoard,
  readRawTrigger,
  readTrigger,
  type TriggerBlock,
  triggerYaml,
} from './trigger-yaml';

/** "flaky-test-on-main" as the page's title: "Flaky test on main". */
export const titleOf = (name: string) => {
  const words = name.replace(/-/g, ' ');
  return words.charAt(0).toUpperCase() + words.slice(1);
};

const isNotFound = (error: unknown) => error instanceof ApiError && error.status === 404;

const errorText = (error: unknown) => (error instanceof Error ? error.message : 'Try again in a moment.');

/** Which block a problem's YAML key sits in. A key outside every block is shown above the form. */
function blockOf(key: string | undefined): TriggerBlock | undefined {
  if (key === 'on' || key === 'cron' || key === 'timeZone') return 'on';
  if (key === 'filter') return 'filter';
  if (key === 'only' || key === 'check' || key === 'create' || key === 'limits') return key;
  return undefined;
}

export function TriggerPage() {
  const org = useOrg();
  const listed = useFactory();
  const { trigger: name } = useParams({ from: '/o/$org/f/$factory/intake/$trigger' });
  const { on, check } = useSearch({ from: '/o/$org/f/$factory/intake/$trigger' });
  const factory = useQuery(factoryQuery(org.id, listed.id)).data ?? listed;
  // The text's key follows the running version, which a save moves: keep the old text until the new one arrives, so
  // the editor stays mounted and its "Saved" notice is not lost to the loading state.
  const definition = useQuery({ ...definitionTextQuery(org.id, factory), placeholderData: keepPreviousData });
  const text = definition.data?.text;
  const saved = text === undefined ? undefined : readTrigger(text, name);
  const raw = text === undefined ? undefined : readRawTrigger(text, name);
  const detail = useQuery({ ...intakeTriggerQuery(org.id, factory.id, name), enabled: saved !== undefined });
  const title = titleOf(name);

  // A refetch that fails (the draft file, once a save has published the draft away) keeps the page: only a
  // definition that was never read, or whose new version failed to load, is an error screen.
  if (definition.isError && (definition.data === undefined || definition.isPlaceholderData)) {
    return (
      <>
        <TopBarSlot crumb={title} />
        <Failure what="the definition" onRetry={() => void definition.refetch()} error={definition.error} />
      </>
    );
  }
  if (definition.isPending || text === undefined) {
    return (
      <>
        <TopBarSlot crumb={title} />
        <Loading />
      </>
    );
  }

  const board = readBoard(text);
  const isNew = saved === undefined && on !== undefined;
  if (saved === undefined && !isNew) {
    return (
      <>
        <TopBarSlot crumb={title} />
        <EmptyState title={`No trigger named ${name}`}>
          It is not in {factoryTitle(factory)}&apos;s definition. Add it from the New trigger picker.
        </EmptyState>
      </>
    );
  }
  if (saved !== undefined && detail.isPending) {
    return (
      <>
        <TopBarSlot crumb={title} />
        {saved.check ? <TriggerCheckSkeleton /> : <Loading />}
      </>
    );
  }
  if (detail.isError && !isNotFound(detail.error)) {
    return (
      <>
        <TopBarSlot crumb={title} />
        <Failure what="this trigger" onRetry={() => void detail.refetch()} error={detail.error} />
      </>
    );
  }
  if (saved !== undefined && isNotFound(detail.error)) {
    return (
      <>
        <TopBarSlot crumb={title} />
        <EmptyState title={`No trigger named ${name}`}>
          It is not in {factoryTitle(factory)}&apos;s definition. Add it from the New trigger picker.
        </EmptyState>
      </>
    );
  }

  const blank: Trigger = {
    name,
    on: on ?? '',
    create: { type: board.workTypes[0] ?? '', station: board.stations[0] ?? '' },
    ...(check === undefined ? {} : { check: { agent: check, brief: '' } }),
  };
  return (
    <TriggerEditor
      key={`${name}/${factory.id}`}
      org={org}
      factory={factory}
      text={text}
      saved={saved}
      raw={raw}
      blank={blank}
      board={board}
      detail={saved === undefined ? undefined : detail.data}
    />
  );
}

interface EditorProps {
  org: { id: string; slug: string };
  factory: Factory;
  /** factory.yaml as the page reads it (the draft's, else the running version's). */
  text: string;
  /** The trigger as the definition has it; undefined for one not yet added. */
  saved: Trigger | undefined;
  /** The same trigger as the file writes it, for the YAML panel. */
  raw: Trigger | undefined;
  /** What a new trigger starts as. */
  blank: Trigger;
  board: BoardChoices;
  detail: IntakeTriggerDetail | undefined;
}

function TriggerEditor({ org, factory, text, saved, raw, blank, board, detail }: EditorProps) {
  const queryClient = useQueryClient();
  const base = saved ?? blank;
  const edits = useEdits<Trigger>(base);
  const value = edits.value;
  const dirty = JSON.stringify(value) !== JSON.stringify(base);
  const [problems, setProblems] = useState<DefinitionProblem[]>([]);
  const [lines, setLines] = useState<KeyLines>({});
  const [notice, setNotice] = useState<{ tone: 'ok' | 'rework'; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  // An invalid save leaves this page's own write in the draft. That write is not someone else's edit, so
  // it must not block the fix the problems ask for; it is cleared when the draft is published or moved.
  const [draftIsOurs, setDraftIsOurs] = useState(false);
  const title = titleOf(value.name);
  const draft = useQuery({ ...draftQuery(org.id, factory.id), enabled: factory.draft !== null });
  // A draft with edits of its own (not this page's) blocks the save: publishing it here would publish them too.
  const blocked = factory.draft !== null && (draft.data?.change_count ?? 0) > 0 && !draftIsOurs;
  const hasErrors = problems.some((p) => p.severity === 'error');
  const isCheck = value.check !== undefined;
  const valid =
    value.on !== '' &&
    value.create.type !== '' &&
    value.create.station !== '' &&
    (!isCheck || (value.check?.agent !== '' && value.check?.brief.trim() !== ''));
  const checks = useQuery({
    ...triggerChecksQuery(org.id, factory.id, value.name),
    enabled: saved?.check !== undefined,
  });
  const checking = (checks.data?.checks ?? []).some((c) => c.state === 'queued' || c.state === 'running');
  const [tryProblem, setTryProblem] = useState<string | null>(null);
  // Why Try is unavailable, shown under the title: a new trigger is not saved, unsaved edits are not the saved
  // trigger, and an event trigger has nothing to try before its first event.
  const tryReason = !saved
    ? 'Save the trigger to try it'
    : dirty
      ? 'Save to try your changes'
      : saved.check
        ? checking
          ? 'A check is queued or running'
          : undefined
        : (detail?.events.length ?? 0) === 0
          ? 'No past events to try yet'
          : undefined;
  const failed = notice?.tone === 'rework';

  // The form hands back the whole trigger; each key that differs from the edits so far is one edit.
  const change = (next: Trigger) => {
    for (const key of Object.keys({ ...value, ...next }) as (keyof Trigger)[]) {
      if (JSON.stringify(value[key]) !== JSON.stringify(next[key])) edits.set(key, next[key] as never);
    }
    setProblems([]);
  };
  const set = <K extends keyof Trigger>(key: K, v: Trigger[K]) => change({ ...value, [key]: v });
  const clearMutation = useMutation(clearStateMutation(org.id, factory.id, value.name));
  const clearState = async () => {
    try {
      await clearMutation.mutateAsync();
    } catch (error) {
      throw new Error(
        error instanceof ApiError && error.problem.code === 'still-checking'
          ? 'A check is queued or running. Clear when it is done.'
          : `Could not clear it. ${errorText(error)}`,
      );
    }
    await queryClient.invalidateQueries({ queryKey: intakeKeys.trigger(org.id, factory.id, value.name) });
  };

  // A problem's line names the field whose lines hold it; the field names its block.
  const blockProblems: BlockProblems = {};
  const above: string[] = [];
  for (const problem of problems) {
    if (problem.file && problem.file !== definitionFile) continue;
    const key = Object.entries(lines).find(([, r]) => r && problem.line >= r.from && problem.line <= r.to)?.[0];
    const block = blockOf(key);
    if (block) blockProblems[block] = [...(blockProblems[block] ?? []), problem.message];
    else above.push(problem.message);
  }

  async function save() {
    setBusy(true);
    setNotice(null);
    try {
      const outcome = await saveTrigger(org.id, factory.id, value, factory.current?.id ?? null);
      if (outcome.kind === 'invalid') {
        setProblems(outcome.problems);
        setLines(keyLines(outcome.text, value.name));
        setDraftIsOurs(true);
        return;
      }
      setDraftIsOurs(false);
      edits.reset();
      setNotice({
        tone: 'ok',
        text: `Saved. ${factoryTitle(factory)}'s definition is version ${outcome.version.number ?? ''} now.`,
      });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: defKeys.factory(org.id, factory.id) }),
        queryClient.invalidateQueries({ queryKey: defKeys.draft(org.id, factory.id) }),
        queryClient.invalidateQueries({ queryKey: defKeys.versions(org.id, factory.id) }),
        queryClient.invalidateQueries({ queryKey: intakeKeys.root(org.id, factory.id) }),
      ]);
    } catch (error) {
      if (error instanceof ApiError && error.status === 412) {
        // Someone else saved the draft: reload what it says now and keep the typed edits.
        setDraftIsOurs(false);
        await queryClient.invalidateQueries({ queryKey: defKeys.draft(org.id, factory.id) });
        setNotice({
          tone: 'rework',
          text: 'The definition changed while you were editing. Your edits are kept: save again to publish them.',
        });
      } else {
        setNotice({ tone: 'rework', text: `The trigger was not saved. ${errorText(error)}` });
      }
    } finally {
      setBusy(false);
    }
  }

  const startLine = keyLines(text, value.name).name?.from;
  const actions = (
    <>
      {isCheck ? (
        <TryCheck
          org={org.id}
          factory={factory.id}
          trigger={value.name}
          disabled={!saved || dirty}
          checking={checking}
          readAt={checks.dataUpdatedAt}
          onProblem={setTryProblem}
        />
      ) : (
        <TryWithPastEvent
          org={org.id}
          factory={factory.id}
          trigger={value.name}
          events={detail?.events ?? []}
          title={value.on === 'schedule' ? 'Try with a past event' : 'Try with a delivery'}
          disabled={tryReason !== undefined}
        />
      )}
      <NotAvailable label="More actions">
        <Button variant="white" size="icon" aria-label="More actions">
          <MoreHorizontal size={16} strokeWidth={1.5} aria-hidden="true" />
        </Button>
      </NotAvailable>
      <Button variant="primary" disabled={!dirty || busy || blocked || hasErrors || !valid} onClick={() => void save()}>
        {notice?.tone === 'ok' && !dirty && !busy ? (
          <Check size={16} strokeWidth={1.5} aria-hidden="true" className="motion-safe:animate-success-check" />
        ) : null}
        {busy ? 'Saving…' : 'Save changes'}
      </Button>
    </>
  );

  return (
    <>
      <TopBarSlot crumb={title} actions={actions} />
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          <h1 className="m-0 text-page-title">{title}</h1>
          <div className="flex items-center gap-2 text-secondary font-semibold">
            <Switch
              aria-label={`${title} on`}
              checked={value.enabled !== false}
              onCheckedChange={(on) => set('enabled', on ? undefined : false)}
            />
            <span>{value.enabled === false ? 'Off' : 'On'}</span>
          </div>
          {isCheck && checks.data ? (
            <TodaySummary checks={checks.data.checks} className="m-0 ml-auto text-secondary text-ink-soft" />
          ) : detail && !isCheck ? (
            <WeekSummary week={detail.week} className="m-0 ml-auto text-secondary text-ink-soft" />
          ) : null}
        </div>
        {(tryProblem ?? tryReason) ? <p className="m-0 -mt-2 text-meta text-muted">{tryProblem ?? tryReason}</p> : null}

        {blocked ? (
          <Callout tone="note">
            {factoryTitle(factory)} has an open draft with edits. Publish or discard it in the definition editor, then
            save this trigger.{' '}
            <Link
              to="/o/$org/f/$factory/definition"
              params={{ org: org.slug, factory: factory.name }}
              className="font-semibold text-run-ink"
            >
              Open the definition editor
            </Link>
          </Callout>
        ) : null}
        {notice ? (
          <div role={failed ? 'alert' : 'status'}>
            <Callout tone={notice.tone}>{notice.text}</Callout>
          </div>
        ) : null}
        {above.length ? (
          <div role="alert" className="flex flex-col gap-1">
            {above.map((message) => (
              <Callout key={message} tone="rework">
                {message}
              </Callout>
            ))}
          </div>
        ) : null}

        <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,360px)]">
          <TriggerForm
            value={value}
            onChange={change}
            board={board}
            factoryName={factoryTitle(factory)}
            problems={blockProblems}
            memory={detail?.memory}
            onClear={saved?.check ? clearState : undefined}
          />
          <aside className="flex min-w-0 flex-col gap-4">
            {isCheck ? (
              checks.isPending && saved ? (
                <TriggerCheckSkeleton />
              ) : (
                <RecentChecks
                  checks={checks.data?.checks ?? []}
                  org={org.slug}
                  factory={factory.name}
                  every={value.cron ? cronWords(value.cron) : undefined}
                />
              )
            ) : (
              <RecentEvents events={detail?.events ?? []} />
            )}
            <section
              aria-labelledby="in-the-definition"
              className="flex flex-col gap-3 rounded-card border border-line p-4"
            >
              <div className="flex items-center gap-3">
                <h2 id="in-the-definition" className="m-0 text-heading">
                  In the definition
                </h2>
                <InfoTip label="About saving">
                  {`Saving publishes a new version of ${factoryTitle(factory)}’s definition.`}
                </InfoTip>
                <Link
                  to="/o/$org/f/$factory/definition"
                  params={{ org: org.slug, factory: factory.name }}
                  search={{ mode: 'yaml', file: definitionFile, line: startLine }}
                  className="ml-auto text-secondary font-semibold text-run-ink"
                >
                  Open file
                </Link>
              </div>
              {raw ? (
                <section
                  aria-label="Trigger file"
                  // biome-ignore lint/a11y/noNoninteractiveTabindex: a scrolling region must be reachable by keyboard (axe scrollable-region-focusable).
                  tabIndex={0}
                  className="overflow-x-auto focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-run"
                >
                  <pre className="m-0 w-max min-w-full rounded-cell border border-line bg-panel p-3 font-mono text-meta leading-relaxed text-ink">
                    <HighlightedYaml text={triggerYaml(raw)} />
                  </pre>
                </section>
              ) : (
                <p className="m-0 text-secondary text-muted">Not in the definition yet. Save to add it.</p>
              )}
            </section>
          </aside>
        </div>
      </div>
    </>
  );
}

function Loading() {
  return <TriggerSkeleton />;
}

function Failure({ what, error, onRetry }: { what: string; error: unknown; onRetry: () => void }) {
  return (
    <Callout tone="rework">
      <span>
        Could not load {what}. {errorText(error)}{' '}
      </span>
      <Button variant="white" size="sm" onClick={onRetry}>
        Try again
      </Button>
    </Callout>
  );
}
