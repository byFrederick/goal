// Step 4, "Shape the line" (docs/design/screens/get-started/line.jpg, ADR 0032 decision 4). The factory step 3
// made, opened as its draft: each station as a card with its name, its agent, its copies and WIP limit, its
// lanes and its gate; the models of the agents; and the factory card on the panel. Every value is read from and
// saved through the line editor and the draft (LineEditorProvider, the draft ops), as the wizard's Line step
// does. Nothing is published here: step 5 publishes (ADR 0032 decision 4).

import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { type ReactNode, useId, useRef, useState } from 'react';
import { factoryQuery } from '../../api/queries';
import type { OrgSummary } from '../../api/types';
import type { ModelStation } from '../../shared/definition/model';
import { factoryTitle } from '../../shared/factory';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { Field, Input, NativeSelect } from '../../ui/field';
import { Leaving, useLeaving } from '../../ui/leaving';
import { NotAvailable } from '../../ui/not-available';
import { addStation, moveStation, removeStation, setStationField, setStationName } from '../factory-editor/draft/ops';
import { DraftSessionProvider, useDraft, useDraftSession, useSection } from '../factory-editor/draft/use-draft';
import { useValidation } from '../factory-editor/frame/use-editor';
import { stationLabel } from '../factory-editor/line/labels';
import { LineEditorProvider, useLineEditor } from '../factory-editor/line/line-editor';
import { ModelsBox } from '../factory-editor/line/models-box';
import { type Direction, stepTarget } from '../factory-editor/line/reorder';
import { WipStepper } from '../factory-editor/line/station-row';
import { carriedSearch, type OnboardingSearch } from './code-search';
import { codeCard, FactoryCard, organizationCard, type PreviewStation, previewSlots } from './factory-preview';
import { AddHere, CardControls, useCardFlip } from './line-step-controls';
import { ModelKeyCard, useModelKeySecret } from './model-key-card';
import { OnboardingLayout } from './onboarding-layout';

export function LineStep({ org, search }: { org: OrgSummary; search: OnboardingSearch }) {
  const factoryId = search.factory;
  const factory = useQuery({ ...factoryQuery(org.id, factoryId ?? ''), enabled: factoryId !== undefined });

  if (!factoryId) {
    return <NoFactory org={org} search={search} message="Choose a template first, then shape its line." />;
  }
  if (factory.isError) {
    return <NoFactory org={org} search={search} message="This factory can’t be read. Choose a template again." />;
  }
  if (factory.isPending) return <Loading org={org} />;
  return (
    <DraftSessionProvider org={org.id} factory={factoryId}>
      <Line org={org} search={search} factory={factoryId} />
    </DraftSessionProvider>
  );
}

/** The Line step without a factory: a link back to step 3, which makes one. */
function NoFactory({ org, search, message }: { org: OrgSummary; search: OnboardingSearch; message: string }) {
  return (
    <OnboardingLayout
      step="line"
      org={org}
      back={
        <Button asChild size="lg" variant="secondary">
          <Link
            to="/o/$org/get-started/$step"
            params={{ org: org.slug, step: 'factory' }}
            search={{ ...search, factory: undefined }}
          >
            Back
          </Link>
        </Button>
      }
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 text-kpi tracking-display">Shape the line</h1>
      </div>
      <Callout tone="rework">{message}</Callout>
    </OnboardingLayout>
  );
}

function Loading({ org }: { org: OrgSummary }) {
  return (
    <OnboardingLayout step="line" org={org}>
      <div aria-busy="true">
        <span role="status" className="sr-only">
          Loading
        </span>
      </div>
    </OnboardingLayout>
  );
}

/** The session is read once it is up: until then the step shows its loading state. Without a draft it shows the running version. */
function Line({ org, search, factory }: { org: OrgSummary; search: OnboardingSearch; factory: string }) {
  const ready = useDraft((s) => s.ready);
  if (!ready) return <Loading org={org} />;
  return (
    <LineEditorProvider place="wizard">
      <LineBody org={org} search={search} factory={factory} />
    </LineEditorProvider>
  );
}

function LineBody({ org, search, factory }: { org: OrgSummary; search: OnboardingSearch; factory: string }) {
  const editor = useLineEditor();
  const basics = useSection('basics');
  const factoryDetail = useQuery(factoryQuery(org.id, factory)).data;
  const host = window.location.host;

  const panelStations: PreviewStation[] = editor.stations.map((s) => ({
    id: s.id,
    label: stationLabel(s),
    kind: s.kind,
    wip: s.wip,
  }));

  return (
    <OnboardingLayout
      step="line"
      org={org}
      back={
        <Button asChild size="lg" variant="secondary">
          <Link
            to="/o/$org/get-started/$step"
            params={{ org: org.slug, step: 'code' }}
            search={carriedSearch(search, { factory })}
          >
            Back
          </Link>
        </Button>
      }
      action={<Continue org={org} search={search} factory={factory} />}
      preview={previewSlots({
        organization: organizationCard(org, host),
        code: codeCard({ url: search.url, secret: search.secret, done: true }),
        factory: (
          <FactoryCard
            name={factoryTitle({
              name: basics.name || factoryDetail?.name || '',
              title: basics.title || factoryDetail?.title,
            })}
            purpose={basics.purpose ?? ''}
            stations={panelStations}
          />
        ),
      })}
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 text-kpi tracking-display">Shape the line</h1>
        <p className="m-0 text-heading font-normal text-ink-soft">
          Work moves left to right. Each station has an agent, how many copies of it run at once, and a limit on work in
          progress.
        </p>
      </div>

      <StationCards />
      <Models />
      <ModelKeyCard org={org.id} factory={factory} />
    </OnboardingLayout>
  );
}

/**
 * Continue goes to Budget, which publishes. It is held while the line has no station that runs an agent and while
 * the draft's validation reports an error or has not answered yet, and while factory.yaml has no model key, with one sentence saying what is missing.
 */
function Continue({ org, search, factory }: { org: OrgSummary; search: OnboardingSearch; factory: string }) {
  const editor = useLineEditor();
  const session = useDraftSession();
  const navigate = useNavigate();
  const validation = useValidation();
  const hasDraft = useDraft((s) => s.hasDraft);
  const [leaving, setLeaving] = useState(false);
  const sentenceId = useId();
  const noAgent = !editor.stations.some((s) => s.kind === 'agent');
  const invalid = validation?.problems.some((p) => p.severity === 'error') ?? false;
  const modelKey = useModelKeySecret();
  const sentence = noAgent
    ? 'Add a station that runs an agent, then continue.'
    : invalid
      ? 'Fix the problems in this line, then continue.'
      : modelKey.ready && modelKey.secret === undefined
        ? 'Add the model key, then continue.'
        : undefined;
  const held = sentence !== undefined || (hasDraft && validation === undefined) || !modelKey.ready || leaving;
  return (
    <div className="flex flex-wrap items-center justify-end gap-3">
      {sentence ? (
        <p id={sentenceId} className="m-0 text-secondary text-muted motion-safe:animate-fade-in">
          {sentence}
        </p>
      ) : null}
      <Button
        type="button"
        size="lg"
        variant="primary"
        disabled={held}
        aria-describedby={sentence ? sentenceId : undefined}
        className="[transition-duration:var(--duration-quick),var(--duration-quick),var(--duration-quick),var(--duration-quick),var(--duration-micro)] [transition-property:color,background-color,border-color,opacity,transform]"
        onClick={() => {
          // Leaving unmounts the draft session and drops its pending autosave, so save first (the model key).
          setLeaving(true);
          void session.flush().then(
            () => {
              void navigate({
                to: '/o/$org/get-started/$step',
                params: { org: org.slug, step: 'budget' },
                search: carriedSearch(search, { factory }),
              });
            },
            () => setLeaving(false),
          );
        }}
      >
        Continue
      </Button>
    </div>
  );
}

/** The stations in order, one card each, with "Add a station here" before the first and after each but Done. */
function StationCards() {
  const editor = useLineEditor();
  const flip = useCardFlip();
  // The cards on screen when the step opened do not enter; a card added later does.
  const opened = useRef(new Set(editor.stations.map((s) => editor.keyOf(s.id))));

  const addAt = (after: string | undefined) => {
    const { edit, station } = addStation(editor.line, 'agent', { name: 'New station', ...(after ? { after } : {}) });
    editor.insert(edit, station, after ? undefined : (line) => moveStation(line, station.id, 0));
  };
  const step = (id: string, dir: Direction, pointer: boolean) => {
    const to = stepTarget(editor.stations, id, dir);
    if (to === undefined) return;
    if (pointer) flip.mark();
    editor.move(id, to);
  };
  const addControl = (after: string | undefined, key: string) => (
    <li key={key} className="flex w-6 shrink-0 items-center justify-center">
      <AddHere onAdd={() => addAt(after)} />
    </li>
  );

  return (
    <section
      aria-label="Stations"
      // biome-ignore lint/a11y/noNoninteractiveTabindex: a scrolling region must be reachable by keyboard (axe scrollable-region-focusable).
      tabIndex={0}
      className="relative overflow-x-auto pb-2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-run"
    >
      <ol aria-label="The stations" className="m-0 flex list-none gap-1 w-max min-w-full p-0">
        {editor.viewOnly ? null : addControl(undefined, 'add-first')}
        {editor.stations.flatMap((station) => {
          const key = editor.keyOf(station.id);
          const card = (
            <li
              key={key}
              ref={flip.ref(key)}
              className={cn(
                'flex w-52 shrink-0 flex-col',
                !opened.current.has(key) && 'motion-safe:animate-row-in motion-reduce:animate-fade-in',
              )}
            >
              <StationCardBody station={station} onStep={(dir, pointer) => step(station.id, dir, pointer)} />
            </li>
          );
          return station.kind === 'done' || editor.viewOnly ? [card] : [card, addControl(station.id, `add-${key}`)];
        })}
      </ol>
      <p role="status" aria-live="polite" className="sr-only">
        {editor.announcement}
      </p>
    </section>
  );
}

function StationCardBody({
  station,
  onStep,
}: {
  station: ModelStation;
  onStep: (dir: Direction, pointer: boolean) => void;
}) {
  const editor = useLineEditor();
  const { leaving, leave } = useLeaving(() => {
    editor.edit(removeStation(editor.line, station.id));
    editor.flush();
    editor.announce(`${stationLabel(station)} removed`);
    document.querySelector<HTMLElement>('section[aria-label="Stations"]')?.focus();
  });
  const { line } = editor;
  const label = stationLabel(station);
  const index = editor.stations.findIndex((s) => s.id === station.id);

  const setName = (name: string) => {
    const e = setStationName(line, station.id, name, { followId: editor.isNew(station.id) });
    const next = e.value.stations?.[index]?.id;
    if (next && next !== station.id) editor.renamed(station.id, next);
    editor.edit(e, { coalesce: `${editor.keyOf(station.id)}/name` });
  };
  const setAgent = (agent: string) => editor.edit(setStationField(line, station.id, 'agent', agent || undefined));
  const setCopies = (copies: number | undefined) =>
    editor.edit(
      setStationField(line, station.id, 'agents', copies === undefined || copies === 1 ? undefined : copies),
      {
        coalesce: `${editor.keyOf(station.id)}/agents`,
      },
    );
  const setWip = (wip: number | undefined) =>
    editor.edit(setStationField(line, station.id, 'wip', wip), { coalesce: `${editor.keyOf(station.id)}/wip` });

  const runsAgent = station.kind === 'agent';
  const lanes = Object.entries(station.route ?? {});

  return (
    <Leaving leaving={leaving} className="min-h-full flex-1 [&>div]:flex [&>div]:flex-col">
      <section
        aria-label={label}
        data-station={station.id}
        className="flex min-h-full flex-1 flex-col gap-3 rounded-card border border-line bg-card p-4"
      >
        <Field label="Name">
          <Input
            data-station-name
            value={station.name ?? ''}
            placeholder={station.id}
            onChange={(e) => setName(e.target.value)}
            onBlur={() => editor.flush()}
            autoComplete="off"
          />
        </Field>
        {station.description ? <p className="m-0 text-secondary text-muted">{station.description}</p> : null}

        {runsAgent ? (
          <>
            <Field label="Agent">
              <NativeSelect
                aria-label={`Agent of ${label}`}
                value={station.agent ?? ''}
                onChange={(e) => setAgent(e.target.value)}
              >
                <option value="">Choose an agent</option>
                {editor.agents.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </NativeSelect>
            </Field>
            <div className="flex flex-wrap gap-4">
              <Count label="Copies">
                <WipStepper
                  label={label}
                  noun="copies"
                  wip={station.agents ?? 1}
                  onChange={setCopies}
                  onBlur={() => editor.flush()}
                  invalid={false}
                  describedBy={undefined}
                  readOnly={editor.viewOnly}
                />
              </Count>
              <Count label="WIP">
                <WipStepper
                  label={label}
                  wip={station.wip}
                  onChange={setWip}
                  onBlur={() => editor.flush()}
                  invalid={false}
                  describedBy={undefined}
                  readOnly={editor.viewOnly}
                />
              </Count>
            </div>
          </>
        ) : (
          <p className="m-0 text-secondary text-muted">{kindSentence(station.kind)}</p>
        )}

        {lanes.length > 0 ? (
          <div className="flex flex-col gap-2 border-t border-line pt-3">
            <span className="text-meta text-muted">Lanes</span>
            <ul className="m-0 flex list-none flex-col gap-2 p-0">
              {lanes.map(([lane, agent]) => (
                <li key={lane} className="flex flex-col">
                  <span className="font-control text-secondary">{lane}</span>
                  <span className="text-secondary text-muted">{agent}</span>
                </li>
              ))}
            </ul>
          </div>
        ) : null}

        {station.gate ? (
          <div className="flex flex-col gap-2 border-t border-line pt-3">
            <span className="text-meta text-muted">Gate</span>
            <p className="m-0 text-secondary text-ink-soft">{gateSentence(station, editor.stations)}</p>
          </div>
        ) : null}

        {editor.viewOnly ? null : (
          <CardControls station={station} stations={editor.stations} label={label} onStep={onStep} onRemove={leave} />
        )}
      </section>
    </Leaving>
  );
}

/** A small label over a control: "Copies" or "WIP" in the card. */
function Count({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-2">
      <span className="text-meta text-muted">{label}</span>
      {children}
    </div>
  );
}

function kindSentence(kind: ModelStation['kind']): string {
  switch (kind) {
    case 'queue':
      return 'Queue, no agent';
    case 'human':
      return 'A person decides here';
    case 'done':
      return 'Done';
    case 'agent':
      return '';
  }
}

/** "Approval merges; rejects go back to Build, at most 2 times." */
function gateSentence(station: ModelStation, stations: readonly ModelStation[]): string {
  const gate = station.gate ?? {};
  const approval = station.action === 'merge' ? 'Approval merges' : 'Approval moves work on';
  if (gate.on_reject === undefined) return `${approval}.`;
  const target = stations.find((s) => s.id === gate.on_reject);
  const back = `rejects go back to ${target ? stationLabel(target) : gate.on_reject}`;
  const max = gate.max_rework;
  return `${approval}; ${back}${max ? `, at most ${max} ${max === 1 ? 'time' : 'times'}` : ''}.`;
}

/** The agents and the model each runs on. The model preset is not built yet (its control is disabled). */
function Models() {
  return (
    <ModelsBox
      preset={
        <NotAvailable label="Model preset">
          <Button type="button" variant="secondary" size="sm" className="cursor-not-allowed">
            Not available yet
          </Button>
        </NotAvailable>
      }
    />
  );
}
