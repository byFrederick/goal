// New trigger, step 1 of 2: what starts it (intake-new, MASTER.md's two-pane picker). The left pane lists the
// sources; the right pane has the chosen source's events. Continue goes to the trigger page with the choice in
// the address. On phones the panes take turns, with Back from the details.
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { Search } from 'lucide-react';
import { useId, useRef, useState } from 'react';
import {
  connectionsQuery,
  definitionTextQuery,
  defKeys,
  environmentTextQuery,
  intakeKeys,
  intakeQuery,
} from '../../api/queries';
import type { TriggerSuggestion } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { Dialog, DialogClose, DialogContent } from '../../ui/dialog';
import { InfoTip } from '../../ui/info-tip';
import { Leaving, useLeaving } from '../../ui/leaving';
import { NotAvailable } from '../../ui/not-available';
import { Skeleton } from '../../ui/skeleton';
import { Stepper } from '../../ui/stepper';
import { agentEnvironments } from '../factory-editor/environments/agent-environment';
import { defaultEnvironment, readEnv } from '../factory-editor/environments/env-file';
import { OpenDraftError, setTriggerEnabled } from './set-trigger-enabled';
import {
  defaultEvent,
  freeTriggerName,
  githubEventGroups,
  newTriggerHref,
  type PickerEvent,
  type PickerSource,
  pickerGroups,
  pickerSources,
  type SourceKey,
  sourceByKey,
} from './sources';
import { titleOf } from './trigger-page';
import { draftOpenNotice } from './trigger-toggle';
import { readBoard, readTrigger } from './trigger-yaml';

export function NewTriggerDialog() {
  const navigate = useNavigate();
  const params = useFactoryParams();
  const org = useOrg();
  const factory = useFactory();
  // The definition says which agents have which tools, and which trigger names are taken.
  const definition = useQuery(definitionTextQuery(org.id, factory));
  const text = definition.data?.text ?? '';
  const board = readBoard(text);
  const servers = useCheckServers(text, board.agents);
  // The organization's own connections decide the Connected and Not connected groups; until they load, none is.
  const { data: connections } = useQuery(connectionsQuery(org.id));
  const connectedKinds = new Set((connections ?? []).map((c) => c.kind));
  const groups = pickerGroups(connectedKinds);
  const [source, setSource] = useState<SourceKey>('github');
  const [event, setEvent] = useState<string>(defaultEvent);
  const [filter, setFilter] = useState('');
  /** The tool a check was chosen from; null for Check with an agent itself. */
  const [tool, setTool] = useState<string | null>(null);
  /** Phones show one pane at a time: the sources, then the chosen source's details. */
  const [detailsShown, setDetailsShown] = useState(false);
  const shownServers = servers.servers.filter((s) => s.label.toLowerCase().includes(filter.trim().toLowerCase()));

  const panel = useRef<HTMLDivElement>(null);
  const leave = () => navigate({ to: '/o/$org/f/$factory/intake', params });
  const chosen = sourceByKey[source];
  const on = eventOf(chosen, event);
  const agent =
    tool === null
      ? board.agents[0]?.name
      : (servers.servers.find((s) => s.name === tool)?.agent ?? board.agents[0]?.name);
  const href =
    on === null
      ? null
      : newTriggerHref(
          params.org,
          params.factory,
          freeTriggerName(nameBase(chosen, on), (name) => readTrigger(text, name) !== undefined),
          on,
          chosen.key === 'check' ? (agent ?? '') : undefined,
        );

  return (
    <Dialog open onOpenChange={(open) => !open && void leave()}>
      <DialogContent
        title="New trigger"
        description="Step 1 of 2: what starts it"
        inlineDescription
        initialFocus={panel}
        className="max-w-4xl"
      >
        <TopBarSlot crumb="New trigger" />
        <div ref={panel} tabIndex={-1} className="-mx-5 -mb-5 flex flex-col outline-none">
          <div className="px-5 pb-3">
            <Stepper
              label="New trigger steps"
              steps={[
                { name: 'What starts it', state: 'current' },
                { name: 'Filters, what it creates and its limits', state: 'todo' },
              ]}
            />
          </div>
          <div className="flex min-h-0 flex-col border-t border-line md:flex-row">
            <fieldset
              className={cn(
                'm-0 flex min-w-0 flex-col gap-1 border-0 bg-panel p-3 md:w-64 md:flex-none md:border-r md:border-line',
                detailsShown && 'max-md:hidden',
              )}
            >
              <legend className="sr-only">Sources</legend>
              <label className="mb-2 flex h-11 items-center gap-2 rounded-control ring-1 ring-inset ring-rule md:h-9 bg-card px-3 text-ink-soft">
                <Search aria-hidden="true" className="size-4 flex-none" />
                <span className="sr-only">Search sources</span>
                <input
                  type="search"
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                  placeholder="Search sources"
                  className="h-full min-w-0 flex-1 bg-transparent text-body text-ink outline-none placeholder:text-muted"
                />
              </label>
              {groups.map((group) => {
                const shown = group.keys
                  .map((key) => pickerSources.find((s) => s.key === key))
                  .filter((s): s is PickerSource => s !== undefined)
                  .filter((s) => s.label.toLowerCase().includes(filter.trim().toLowerCase()));
                if (shown.length === 0) return null;
                return (
                  <div key={group.title} className="flex flex-col gap-1">
                    <h3 className="m-0 px-2 pt-2 pb-1 text-meta font-semibold text-muted">{group.title}</h3>
                    {shown.map((s) => (
                      <SourceButton
                        key={s.key}
                        source={s}
                        connectHref={`/o/${params.org}/access`}
                        showConnect={group.title === 'Not connected' && s.available}
                        pressed={source === s.key && tool === null}
                        onPick={() => {
                          setSource(s.key);
                          setTool(null);
                          setEvent(defaultEvent);
                          setDetailsShown(true);
                        }}
                      />
                    ))}
                  </div>
                );
              })}
              {servers.loading || servers.failed || shownServers.length > 0 ? (
                <div className="flex flex-col gap-1">
                  <h3 className="m-0 px-2 pt-2 pb-1 text-meta font-semibold text-muted">Check with an agent</h3>
                  <Skeleton
                    ready={!servers.loading}
                    placeholder={
                      <div className="flex flex-col gap-1">
                        {['one', 'two'].map((k) => (
                          <div key={k} className="flex h-11 items-center px-2 md:h-10">
                            <span className="h-4 w-28 rounded-chip bg-hover" />
                          </div>
                        ))}
                      </div>
                    }
                  >
                    <div className="flex flex-col gap-1">
                      {servers.failed ? (
                        <div role="alert" className="flex items-center gap-2 px-2 text-meta text-rework-ink">
                          <span className="min-w-0 flex-1">Couldn't read the agents' environments.</span>
                          <Button variant="ghost" size="sm" onClick={servers.retry}>
                            Retry
                          </Button>
                        </div>
                      ) : null}
                      {shownServers.map((t) => (
                        <button
                          key={t.name}
                          type="button"
                          aria-pressed={source === 'check' && tool === t.name}
                          onClick={() => {
                            setSource('check');
                            setTool(t.name);
                            setDetailsShown(true);
                          }}
                          className="press flex h-11 min-w-0 shrink-0 grow items-center gap-3 rounded-control px-2 text-left text-body font-medium text-ink transition-colors duration-(--duration-fast) ease-out hover:bg-hover aria-pressed:bg-hover md:h-10"
                        >
                          <span title={t.label} className="min-w-0 flex-1 truncate">
                            {t.label}
                          </span>
                          <Chip>its tool</Chip>
                        </button>
                      ))}
                    </div>
                  </Skeleton>
                </div>
              ) : null}
            </fieldset>

            <div className={cn('flex min-w-0 flex-1 flex-col gap-4 p-5', !detailsShown && 'max-md:hidden')}>
              <Button
                variant="ghost"
                size="sm"
                className="-ml-2 self-start md:hidden"
                onClick={() => setDetailsShown(false)}
              >
                Back
              </Button>
              <div className="flex items-center gap-3">
                <h3 className="m-0 text-heading">
                  {chosen.key === 'github'
                    ? 'GitHub events'
                    : tool !== null
                      ? (servers.servers.find((s) => s.name === tool)?.label ?? tool)
                      : chosen.label}
                </h3>
                {chosen.key === 'check' ? (
                  <InfoTip about="Check with an agent">
                    An agent looks at the source on a schedule, with the tools it has, and files the items it finds. Any
                    source its tools reach.
                  </InfoTip>
                ) : null}
              </div>
              {chosen.key === 'github' ? (
                <GithubEvents event={event} onChange={setEvent} />
              ) : (
                <p className="m-0 text-body text-ink-soft">{sourceNote(chosen, agent)}</p>
              )}
            </div>
          </div>

          <SuggestionsBlock />

          <div className="flex flex-wrap items-center gap-3 border-t border-line p-4">
            <span className="text-secondary text-muted">Next: filters, what it creates and its limits, as a draft</span>
            <div className="ml-auto flex items-center gap-2">
              <DialogClose asChild>
                <Button variant="secondary">Cancel</Button>
              </DialogClose>
              {href ? (
                <Button variant="primary" onClick={() => navigate({ href })}>
                  Continue
                </Button>
              ) : null}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

interface CheckServer {
  /** The server's name in the environment file. */
  name: string;
  /** What the row says: the source's own label when the picker has one for the name, else the name. */
  label: string;
  /** The first agent whose environment has the server: the check's agent. */
  agent: string;
}

/**
 * The MCP servers the factory's agents reach, each once, in the order the agents list them. An agent's
 * environment is the file factory.yaml names (default when it names none); `loading` holds until the files
 * of those environments are read.
 */
function useCheckServers(
  text: string,
  agents: readonly { name: string }[],
): { servers: CheckServer[]; loading: boolean; failed: boolean; retry: () => void } {
  const org = useOrg();
  const factory = useFactory();
  const byAgent = agentEnvironments(text);
  const envOf = (agent: string) => byAgent[agent] ?? defaultEnvironment;
  const names = [...new Set(agents.map((a) => envOf(a.name)))];
  const files = useQueries({ queries: names.map((name) => environmentTextQuery(org.id, factory, name)) });
  const loading = files.some((f) => f.data === undefined && !f.isError);
  const failed = files.some((f) => f.isError);
  const retry = () => {
    for (const f of files) if (f.isError) void f.refetch();
  };
  const mcp = new Map(names.map((name, i) => [name, Object.keys(readEnv(files[i]?.data?.text ?? '').mcp)]));
  const servers: CheckServer[] = [];
  for (const agent of agents) {
    for (const name of mcp.get(envOf(agent.name)) ?? []) {
      if (servers.some((s) => s.name === name)) continue;
      servers.push({ name, label: pickerSources.find((s) => s.key === name)?.label ?? name, agent: agent.name });
    }
  }
  return { servers, loading, failed, retry };
}

/** The trigger's event key for a choice; null when the source has none to choose here (a webhook is chosen later). */
function eventOf(source: PickerSource, event: string): string | null {
  if (source.key === 'github') return event;
  if (source.key === 'schedule' || source.key === 'check') return 'schedule';
  return null;
}

/** The start of a name for the new trigger: new-check, new-schedule, new-check-run-failed. */
function nameBase(source: PickerSource, on: string): string {
  if (source.key === 'check') return 'new-check';
  return `new-${on.replace(/^github\./, '').replace(/[._]/g, '-')}`;
}

/**
 * The factory's disabled triggers, from the API, as starting points. Turn on enables one through the same
 * write as Intake's switch; the row collapses away once the write is answered and intake is read again.
 */
function SuggestionsBlock() {
  const org = useOrg();
  const factory = useFactory();
  const intake = useQuery(intakeQuery(org.id, factory.id));
  if (intake.isError) return null;
  const shown = intake.data?.suggestions ?? [];
  if (intake.data && shown.length === 0) return null;

  return (
    <section aria-labelledby="suggestions-heading" className="flex flex-col gap-2 border-t border-line p-4">
      <div className="flex items-center gap-1">
        <h3 id="suggestions-heading" className="m-0 text-meta font-semibold text-muted">
          Or turn on a suggestion from {factory.title}&apos;s definition
        </h3>
        <InfoTip about="Suggestions">
          Suggestions are the triggers in the definition that are off. Turning one on publishes a new version.
        </InfoTip>
      </div>
      <Skeleton ready={intake.data !== undefined} placeholder={<SuggestionsPlaceholder />}>
        <ul className="m-0 flex list-none flex-col p-0">
          {shown.map((suggestion) => (
            <SuggestionRow key={suggestion.name} suggestion={suggestion} />
          ))}
        </ul>
      </Skeleton>
    </section>
  );
}

function SuggestionsPlaceholder() {
  return (
    <div className="flex flex-col">
      {[0, 1].map((n) => (
        <div key={n} className="flex min-h-10 items-center gap-3 border-t border-line py-2">
          <span className="h-5 w-16 rounded-chip bg-hover" />
          <span className="h-4 flex-1 rounded-chip bg-hover" />
          <span className="h-8 w-20 rounded-control bg-hover" />
        </div>
      ))}
    </div>
  );
}

function SuggestionRow({ suggestion }: { suggestion: TriggerSuggestion }) {
  const org = useOrg();
  const factory = useFactory();
  const client = useQueryClient();
  const [notice, setNotice] = useState<string | null>(null);
  const { leaving, leave } = useLeaving(() => {
    void client.invalidateQueries({ queryKey: intakeKeys.root(org.id, factory.id) });
    void client.invalidateQueries({ queryKey: defKeys.factories(org.id) });
  });
  const turnOn = useMutation({
    mutationFn: () =>
      setTriggerEnabled({
        org: org.id,
        factory: factory.id,
        currentVersionId: factory.current?.id ?? null,
        trigger: suggestion.name,
        enabled: true,
      }),
    onSuccess: leave,
    onError: (error) =>
      setNotice(
        error instanceof OpenDraftError
          ? draftOpenNotice
          : `Not turned on. ${error instanceof Error ? error.message : 'Try again.'}`,
      ),
  });
  const blocked = factory.draft !== null;
  return (
    <li className="border-t border-line">
      <Leaving leaving={leaving}>
        <div className="flex min-h-10 items-center gap-3 py-2">
          <Chip>{suggestion.create.station}</Chip>
          <span className="min-w-0 flex-1 text-secondary text-ink">{titleOf(suggestion.name)}</span>
          <Button
            size="sm"
            disabled={turnOn.isPending || leaving}
            onClick={() => {
              if (blocked) {
                setNotice(draftOpenNotice);
                return;
              }
              setNotice(null);
              turnOn.mutate();
            }}
          >
            Turn on
          </Button>
        </div>
        {notice ? (
          <p role="alert" className="m-0 pb-2 text-meta text-rework-ink">
            {notice}
          </p>
        ) : null}
      </Leaving>
    </li>
  );
}

function sourceNote(source: PickerSource, agent: string | undefined): string {
  if (source.key === 'schedule') return 'Starts on a schedule. The time and its time zone are set on the next step.';
  if (source.key === 'check')
    return agent
      ? `Starts on a schedule. ${agent} looks at the source each time and files what it finds. The brief is written on the next step.`
      : 'Starts on a schedule. An agent looks at the source each time. The factory has no agent yet.';
  return 'Not available yet. Webhook triggers are set up from the organization’s webhooks.';
}

function SourceButton({
  source,
  pressed,
  onPick,
  connectHref,
  showConnect,
}: {
  source: PickerSource;
  pressed: boolean;
  onPick: () => void;
  connectHref: string;
  showConnect: boolean;
}) {
  const button = (
    <button
      type="button"
      aria-pressed={pressed}
      aria-disabled={source.available ? undefined : 'true'}
      onClick={() => {
        if (source.available) onPick();
      }}
      className={cn(
        'flex h-11 min-w-0 shrink-0 grow md:h-10 items-center gap-3 rounded-control px-2 text-left text-body font-medium text-ink hover:bg-hover',
        'aria-pressed:bg-hover',
        !source.available && 'text-muted',
      )}
    >
      <SourceTile source={source} />
      {source.label}
    </button>
  );
  const control = source.available ? button : <NotAvailable label={source.label}>{button}</NotAvailable>;
  if (!showConnect) return control;
  return (
    <div className="flex items-center gap-1">
      {control}
      <a
        href={connectHref}
        aria-label={`Connect ${source.label}`}
        className="flex-none rounded-control px-2 py-1 text-secondary font-medium text-run-ink no-underline hover:bg-hover"
      >
        Connect
      </a>
    </div>
  );
}

function SourceTile({ source }: { source: PickerSource }) {
  const Icon = source.icon;
  return (
    <span aria-hidden="true" className="flex size-4 flex-none items-center justify-center text-ink-soft">
      <Icon className="size-4" />
    </span>
  );
}

/** The GitHub events the picture lists; only the two the schema accepts can be chosen. */
function GithubEvents({ event, onChange }: { event: string; onChange: (event: string) => void }) {
  return (
    <div className="flex flex-col gap-4">
      {githubEventGroups.map((group) => (
        <fieldset key={group.title} className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
          <legend className="mb-2 p-0 text-meta font-semibold text-muted">{group.title}</legend>
          <div className="grid gap-2 sm:grid-cols-2">
            {group.events.map((ev) => (
              <EventOption
                key={ev.label}
                event={ev}
                checked={ev.key !== null && ev.key === event}
                onChange={onChange}
              />
            ))}
          </div>
        </fieldset>
      ))}
    </div>
  );
}

function EventOption({
  event,
  checked,
  onChange,
}: {
  event: PickerEvent;
  checked: boolean;
  onChange: (event: string) => void;
}) {
  const id = useId();
  const disabled = event.key === null;
  const input = (
    <input
      id={id}
      type="radio"
      name="github-event"
      value={event.key ?? event.label}
      checked={checked}
      aria-disabled={disabled ? 'true' : undefined}
      // Controlled: an arrow key that moves onto a disabled event must not choose it.
      onChange={() => {
        if (event.key) onChange(event.key);
      }}
      className="m-0 size-4 flex-none cursor-pointer accent-run"
    />
  );
  return (
    <div
      className={cn(
        'flex min-h-11 items-center gap-3 rounded-cell md:min-h-10 border border-line px-3 text-secondary text-ink',
        checked && 'border-ink bg-card text-ink',
        disabled && 'text-muted',
      )}
    >
      {disabled ? <NotAvailable label={event.label}>{input}</NotAvailable> : input}
      <label htmlFor={id} className={cn('flex-1 py-2', disabled ? 'cursor-not-allowed' : 'cursor-pointer')}>
        {event.label}
      </label>
    </div>
  );
}
