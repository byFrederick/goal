// One intake trigger (ADR 0026; the picture is docs/design/screens/intake/trigger.jpg): its title with
// the on/off switch, this week's results, the When, Only if, Create and Limits blocks, its recent events
// and its YAML in the definition. Save changes writes spec.board.triggers through the draft files API and
// publishes a new version (trigger-save.ts). A name the definition does not have opens a blank trigger
// when the URL carries the event the New trigger picker chose (?on=<event key>).
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useParams, useSearch } from '@tanstack/react-router';
import { MoreHorizontal, Play } from 'lucide-react';
import { useState } from 'react';
import { ApiError } from '../../api/client';
import { definitionFile } from '../../api/intake';
import {
  definitionTextQuery,
  defKeys,
  draftQuery,
  factoryQuery,
  intakeKeys,
  intakeTriggerQuery,
} from '../../api/queries';
import type { DefinitionProblem, Factory, IntakeTriggerDetail } from '../../api/types';
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
import { RecentEvents, weekLine } from './trigger-events';
import { type BlockProblems, TriggerForm } from './trigger-form';
import { saveTrigger } from './trigger-save';
import {
  type BoardChoices,
  type KeyLines,
  keyLines,
  readBoard,
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
  if (key === 'only' || key === 'create' || key === 'limits') return key;
  return undefined;
}

export function TriggerPage() {
  const org = useOrg();
  const listed = useFactory();
  const { trigger: name } = useParams({ from: '/o/$org/f/$factory/intake/$trigger' });
  const { on } = useSearch({ from: '/o/$org/f/$factory/intake/$trigger' });
  const factory = useQuery(factoryQuery(org.id, listed.id)).data ?? listed;
  const definition = useQuery(definitionTextQuery(org.id, factory));
  const text = definition.data?.text;
  const saved = text === undefined ? undefined : readTrigger(text, name);
  const detail = useQuery({ ...intakeTriggerQuery(org.id, factory.id, name), enabled: saved !== undefined });
  const title = titleOf(name);

  if (definition.isError) {
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
        <Loading />
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
  };
  return (
    <TriggerEditor
      key={`${name}/${factory.id}`}
      org={org}
      factory={factory}
      text={text}
      saved={saved}
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
  /** What a new trigger starts as. */
  blank: Trigger;
  board: BoardChoices;
  detail: IntakeTriggerDetail | undefined;
}

function TriggerEditor({ org, factory, text, saved, blank, board, detail }: EditorProps) {
  const queryClient = useQueryClient();
  const base = saved ?? blank;
  const edits = useEdits<Trigger>(base);
  const value = edits.value;
  const dirty = JSON.stringify(value) !== JSON.stringify(base);
  const [problems, setProblems] = useState<DefinitionProblem[]>([]);
  const [lines, setLines] = useState<KeyLines>({});
  const [notice, setNotice] = useState<{ tone: 'ok' | 'rework'; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const title = titleOf(value.name);
  const draft = useQuery({ ...draftQuery(org.id, factory.id), enabled: factory.draft !== null });
  // A draft with edits of its own (not this page's) blocks the save: publishing it here would publish them too.
  const blocked = factory.draft !== null && (draft.data?.change_count ?? 0) > 0;
  const hasErrors = problems.some((p) => p.severity === 'error');
  const valid = value.on !== '' && value.create.type !== '' && value.create.station !== '';
  const failed = notice?.tone === 'rework';

  const set = <K extends keyof Trigger>(key: K, v: Trigger[K]) => {
    edits.set(key, v);
    setProblems([]);
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
        return;
      }
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
      <NotAvailable label="Try with a past event">
        <Button variant="white" size="md">
          <Play size={15} strokeWidth={1.5} aria-hidden="true" />
          Try with a past event
        </Button>
      </NotAvailable>
      <NotAvailable label="More actions">
        <Button variant="white" size="icon" aria-label="More actions">
          <MoreHorizontal size={16} strokeWidth={1.5} aria-hidden="true" />
        </Button>
      </NotAvailable>
      <Button variant="primary" disabled={!dirty || busy || blocked || hasErrors || !valid} onClick={() => void save()}>
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
          {detail ? <p className="m-0 ml-auto text-secondary text-muted">{weekLine(detail.week)}</p> : null}
        </div>

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
            set={set}
            board={board}
            factoryName={factoryTitle(factory)}
            problems={blockProblems}
          />
          <aside className="flex min-w-0 flex-col gap-4">
            <RecentEvents events={detail?.events ?? []} />
            <section
              aria-labelledby="in-the-definition"
              className="flex flex-col gap-3 rounded-card border border-line p-4"
            >
              <div className="flex items-center gap-3">
                <h2 id="in-the-definition" className="m-0 text-heading">
                  In the definition
                </h2>
                <Link
                  to="/o/$org/f/$factory/definition"
                  params={{ org: org.slug, factory: factory.name }}
                  search={{ mode: 'yaml', file: definitionFile, line: startLine }}
                  className="ml-auto text-secondary font-semibold text-run-ink"
                >
                  Open file
                </Link>
              </div>
              {saved ? (
                <pre className="m-0 overflow-x-auto rounded-cell bg-andon p-3 font-mono text-meta leading-relaxed text-andon-text">
                  {triggerYaml(saved)}
                </pre>
              ) : (
                <p className="m-0 text-secondary text-muted">Not in the definition yet. Save to add it.</p>
              )}
              <p className="m-0 text-meta text-muted">
                Saving publishes a new version of {factoryTitle(factory)}&apos;s definition.
              </p>
            </section>
          </aside>
        </div>
      </div>
    </>
  );
}

function Loading() {
  return (
    <div aria-busy="true" className="min-h-40">
      <span className="sr-only">Loading</span>
    </div>
  );
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
