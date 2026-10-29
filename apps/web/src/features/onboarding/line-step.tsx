// Step 4, "Shape the line" (docs/design/screens/get-started/line.jpg, ADR 0032 decision 4). The factory step 3
// made, opened as its draft: each station as a card with its name, its agent, its copies and WIP limit, its
// lanes and its gate; the models of the agents; and the factory card on the panel. Every value is read from and
// saved through the line editor and the draft (LineEditorProvider, the draft ops), as the wizard's Line step
// does. Nothing is published here: step 5 publishes (ADR 0032 decision 4).
import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import { factoryQuery } from '../../api/queries';
import type { OrgSummary } from '../../api/types';
import type { ModelStation } from '../../shared/definition/model';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Field, Input, NativeSelect } from '../../ui/field';
import { NotAvailable } from '../../ui/not-available';
import { setStationField, setStationName } from '../factory-editor/draft/ops';
import { DraftSessionProvider, useDraft, useSection } from '../factory-editor/draft/use-draft';
import { stationLabel } from '../factory-editor/line/labels';
import { LineEditorProvider, useLineEditor } from '../factory-editor/line/line-editor';
import { ModelsBox } from '../factory-editor/line/models-box';
import { WipStepper } from '../factory-editor/line/station-row';
import type { OnboardingSearch } from './code-search';
import { codeCard, FactoryCard, organizationCard, type PreviewStation, previewSlots } from './factory-preview';
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
        <h1 className="m-0 font-numeral text-kpi tracking-display">Shape the line</h1>
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

/** The draft is read once it is open: until then the step shows its loading state. */
function Line({ org, search, factory }: { org: OrgSummary; search: OnboardingSearch; factory: string }) {
  const ready = useDraft((s) => s.ready);
  const hasDraft = useDraft((s) => s.hasDraft);
  if (!ready) return <Loading org={org} />;
  if (!hasDraft) {
    return (
      <NoFactory org={org} search={search} message="This factory has no draft to shape. Choose a template again." />
    );
  }
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
            params={{ org: org.slug, step: 'factory' }}
            search={{ connection: search.connection, owner: search.owner, repos: search.repos, factory }}
          >
            Back
          </Link>
        </Button>
      }
      action={
        <Button asChild size="lg" variant="primary">
          <Link to="/o/$org/get-started/$step" params={{ org: org.slug, step: 'budget' }} search={{ factory }}>
            Continue
          </Link>
        </Button>
      }
      preview={previewSlots({
        organization: organizationCard(org, host),
        code: codeCard(search.repos?.[0]),
        factory: (
          <FactoryCard
            name={basics.name || factoryDetail?.name || ''}
            purpose={basics.purpose ?? ''}
            stations={panelStations}
          />
        ),
      })}
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 font-numeral text-kpi tracking-display">Shape the line</h1>
        <p className="m-0 text-heading font-normal text-ink-soft">
          Work moves left to right. Each station has an agent, how many copies of it run at once, and a limit on work in
          progress.
        </p>
      </div>

      <StationCards />
      <Models />
    </OnboardingLayout>
  );
}

/** The stations in order, one card each. Stations that run an agent show its copies and WIP limit. */
function StationCards() {
  const editor = useLineEditor();
  return (
    <ol aria-label="The stations" className="m-0 flex list-none gap-3 overflow-x-auto p-0 pb-2">
      {editor.stations.map((station) => (
        <li key={editor.keyOf(station.id)} className="flex w-52 shrink-0 flex-col">
          <StationCardBody station={station} />
        </li>
      ))}
    </ol>
  );
}

function StationCardBody({ station }: { station: ModelStation }) {
  const editor = useLineEditor();
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
    <section
      aria-label={label}
      data-station={station.id}
      className="flex min-h-full flex-col gap-3 rounded-card border border-line bg-card p-4"
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
    </section>
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
