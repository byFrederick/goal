// Step 2, "Start your first factory" (docs/design/screens/get-started/template.jpg, ADR 0032 decision 4). The
// catalog's templates as cards (the first is chosen), the factory's name and what it does, and "Use <template>",
// which creates the factory from the template with POST /factories. The purpose then goes into the new draft;
// only then does Continue reach step 3 (Code, ADR 0041 decision 9) with ?factory=<id>&template=<name>. The template list, the create call and the draft are the wizard's
// (factory-editor/start and draft): nothing here is a second copy of them.

import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { useEffect, useRef, useState } from 'react';
import { ApiError } from '../../api/client';
import { createFactory } from '../../api/definitions';
import { factoriesQuery, keys, templateQuery } from '../../api/queries';
import type { Factory, OrgSummary, TemplateSummary } from '../../api/types';
import { modelOf } from '../../shared/definition/model';
import { factoryTitle } from '../../shared/factory';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Chip } from '../../ui/chip';
import { Field, Input, Textarea } from '../../ui/field';
import { NotAvailable } from '../../ui/not-available';
import { RadioCards } from '../../ui/radio-cards';
import { setBasics } from '../factory-editor/draft/ops';
import { DraftSessionProvider, useDraft, useDraftSession } from '../factory-editor/draft/use-draft';
import { stationLabel } from '../factory-editor/line/labels';
import { nameFromTitle, nameProblem, newFactoryRequest, suggestedTitle } from '../factory-editor/start/start-choice';
import { useTemplates } from '../factory-editor/start/template-picker';
import { type OnboardingSearch, repoOf } from './code-search';
import {
  codeCard,
  FactoryCard,
  FactoryInitial,
  organizationCard,
  type PreviewStation,
  previewSlots,
} from './factory-preview';
import { OnboardingLayout } from './onboarding-layout';

/** The catalog's name of the blank start, which the card "Start empty" picks. */
const blank = 'blank';

/** What this step hands on once the factory exists: the factory, its template and what the person wrote here. */
interface Created {
  factory: Factory;
  template: string;
  purpose: string;
}

type Panel = ReturnType<typeof previewSlots>;

/** The URL state the steps carry: the factory once this step made it, its template, and the Code step's choice. */
function searchOf(search: OnboardingSearch, factory?: string, template?: string): OnboardingSearch {
  return { url: search.url, secret: search.secret, template: template ?? search.template, factory };
}

export function FactoryStep({ org, search }: { org: OrgSummary; search: OnboardingSearch }) {
  const [created, setCreated] = useState<Created>();
  const [choice, setChoice] = useState<string>();
  const { templates, blank: blankTemplate, isPending, error, refetch } = useTemplates(org.id);
  const catalog = blankTemplate ? [...templates, blankTemplate] : templates;
  const lines = useQueries({ queries: catalog.map((t) => templateQuery(org.id, t.name)) });
  // A template's line as the panel and the cards show it: its stations in order, with their WIP limits.
  const stationsOf = (name: string): PreviewStation[] => {
    const i = catalog.findIndex((t) => t.name === name);
    const data = lines[i]?.data;
    const stations = data ? (modelOf(data.model).line.stations ?? []) : [];
    return stations.map((s) => ({ id: s.id, label: stationLabel(s), kind: s.kind, wip: s.wip }));
  };

  const picked = choice ?? templates[0]?.name ?? blank;
  const pickedTitle = picked === blank ? 'Start empty' : (templates.find((t) => t.name === picked)?.title ?? picked);
  const organization = organizationCard(org, window.location.host);
  const code = codeCard(repoOf(search.url), search.secret);

  if (search.factory) return <ContinueFactory org={org} search={search} />;
  if (created) {
    return (
      <SaveSetup
        org={org}
        search={search}
        created={created}
        preview={previewSlots({
          organization,
          code,
          factory: (
            <FactoryCard name={factoryTitle(created.factory)} purpose={created.purpose} stations={stationsOf(picked)} />
          ),
        })}
      />
    );
  }

  return (
    <CreateFactory
      org={org}
      search={search}
      picked={picked}
      pickedTitle={pickedTitle}
      onPick={setChoice}
      templatesLoading={isPending}
      templatesError={error}
      onRetry={() => void refetch()}
      templates={templates}
      blank={blankTemplate}
      stationsOf={stationsOf}
      onCreated={setCreated}
      panel={(name, purpose) =>
        previewSlots({
          organization,
          code,
          factory: <FactoryCard name={name} purpose={purpose} stations={stationsOf(picked)} />,
        })
      }
    />
  );
}

/** Step 3 with a factory already made (Back from step 4): the factory is kept, not made again. */
function ContinueFactory({ org, search }: { org: OrgSummary; search: OnboardingSearch }) {
  return (
    <OnboardingLayout
      step="factory"
      org={org}
      back={
        <Button asChild size="lg" variant="secondary">
          <Link to="/get-started">Back</Link>
        </Button>
      }
      action={
        <Button asChild size="lg" variant="primary">
          <Link
            to="/o/$org/get-started/$step"
            params={{ org: org.slug, step: 'code' }}
            search={searchOf(search, search.factory)}
          >
            Continue
          </Link>
        </Button>
      }
      preview={{
        organization: organizationCard(org, window.location.host),
        second: codeCard(repoOf(search.url), search.secret),
      }}
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 text-kpi tracking-display">Start your first factory</h1>
        <p className="m-0 text-heading font-normal text-ink-soft">Your factory is made. Connect its code next.</p>
      </div>
    </OnboardingLayout>
  );
}

interface CreateFactoryProps {
  org: OrgSummary;
  search: OnboardingSearch;
  picked: string;
  pickedTitle: string;
  onPick: (name: string) => void;
  templatesLoading: boolean;
  templatesError: unknown;
  onRetry: () => void;
  templates: TemplateSummary[];
  blank: TemplateSummary | undefined;
  stationsOf: (name: string) => PreviewStation[];
  onCreated: (created: Created) => void;
  /** The panel for the typed name and purpose: the chosen template's line, before the factory exists. */
  panel: (name: string, purpose: string) => Panel;
}

/** The form: the template cards, the name and purpose, and the create button. Before the factory exists. */
function CreateFactory({
  org,
  search,
  picked,
  pickedTitle,
  onPick,
  templatesLoading,
  templatesError,
  onRetry,
  templates,
  blank: blankTemplate,
  stationsOf,
  onCreated,
  panel,
}: CreateFactoryProps) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const nameRef = useRef<HTMLInputElement>(null);
  const taken = (useQuery(factoriesQuery(org.id)).data ?? []).map((f) => f.name);

  const [typedTitle, setTypedTitle] = useState<string>();
  const [purpose, setPurpose] = useState('');
  // The name goald refused as taken: the name field says so and takes focus.
  const [takenName, setTakenName] = useState<string | null>(null);
  const [error, setError] = useState<string>();

  const kind = picked === blank ? 'blank' : 'template';
  const title = typedTitle ?? suggestedTitle(kind, pickedTitle);
  const trimmed = title.trim();
  const name = nameFromTitle(trimmed, taken);
  const nameError =
    takenName !== null && takenName === trimmed
      ? `${org.name} already has a factory named ${trimmed}. Choose another name.`
      : trimmed !== '' && typedTitle !== undefined
        ? nameProblem(name)
          ? 'Use a title with at least 3 letters or digits.'
          : undefined
        : undefined;

  const request = newFactoryRequest({ kind, name, title: trimmed, template: kind === 'template' ? picked : undefined });
  const ready = request !== null && !nameError && !templatesLoading && !templatesError;

  const create = useMutation({
    mutationFn: () => {
      if (!request) throw new Error('The factory has no name yet');
      return createFactory(org.id, request);
    },
    onSuccess: (factory) => {
      void client.invalidateQueries({ queryKey: keys.factories(org.id) });
      void client.invalidateQueries({ queryKey: keys.overviews(org.id) });
      const text = purpose.trim();
      // Nothing to save beyond the factory itself: the Code step opens at once.
      if (text === '') {
        void navigate({
          to: '/o/$org/get-started/$step',
          params: { org: org.slug, step: 'code' },
          search: searchOf(search, factory.id, picked),
        });
        return;
      }
      onCreated({ factory, template: picked, purpose: text });
    },
    onError: (err) => {
      if (err instanceof ApiError && err.code === 'name-taken') {
        setTakenName(trimmed);
        setError(undefined);
        nameRef.current?.focus();
        return;
      }
      setError(err instanceof ApiError ? (err.detail ?? err.title) : 'goald can’t be reached. Try again in a moment.');
    },
  });

  const options = [
    ...templates.map((t) => ({
      value: t.name,
      label: t.title,
      description: <TemplateLine summary={t.summary} stations={stationsOf(t.name)} />,
    })),
    {
      value: blank,
      label: 'Start empty',
      description: (
        <TemplateLine
          summary={blankTemplate?.summary ?? 'One station and one agent. Shape it yourself.'}
          stations={stationsOf(blank)}
        />
      ),
    },
  ];

  return (
    <OnboardingLayout
      step="factory"
      org={org}
      back={
        <Button asChild size="lg" variant="secondary">
          <Link to="/get-started">Back</Link>
        </Button>
      }
      action={
        <Button
          type="button"
          size="lg"
          variant="primary"
          busy={create.isPending}
          busyLabel="Creating…"
          disabled={!ready}
          onClick={() => {
            if (ready) create.mutate();
          }}
        >
          Use {pickedTitle}
        </Button>
      }
      preview={panel(trimmed, purpose)}
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 text-kpi tracking-display">Start your first factory</h1>
        <p className="m-0 text-heading font-normal text-ink-soft">
          A factory is one line of stations staffed by agents. Start from a template and change anything.
        </p>
      </div>

      {templatesError ? (
        <div role="alert" className="flex flex-col items-start gap-3">
          <Callout tone="rework">The templates can’t be read right now. Try again in a moment.</Callout>
          <Button variant="secondary" onClick={onRetry}>
            Retry
          </Button>
        </div>
      ) : templatesLoading ? (
        <div aria-busy="true">
          <span role="status" className="sr-only">
            Loading
          </span>
        </div>
      ) : (
        <>
          {templates.length === 0 ? (
            <p className="m-0 text-secondary text-muted">
              No templates in the bucket yet. Start empty, then shape the line in the next step.
            </p>
          ) : null}
          <RadioCards legend="Template" name="template" value={picked} onChange={onPick} options={options} />
        </>
      )}

      <div className="flex flex-col gap-6">
        <Field label="Factory name" error={nameError}>
          <div className="flex items-center gap-3">
            <Input
              ref={nameRef}
              value={title}
              onChange={(e) => {
                setTakenName(null);
                setTypedTitle(e.target.value);
              }}
              autoComplete="off"
              className="min-w-0 flex-1"
            />
            <FactoryInitial name={trimmed} className="size-11 text-heading" />
          </div>
        </Field>

        <Field label="What it does">
          <Textarea
            rows={3}
            value={purpose}
            onChange={(e) => setPurpose(e.target.value)}
            placeholder="What the factory works on"
          />
        </Field>

        <div className="flex flex-col gap-2">
          <span className="text-sm font-medium">Colour</span>
          <div>
            <NotAvailable label="Colour">
              <Button type="button" variant="secondary">
                Not available yet
              </Button>
            </NotAvailable>
          </div>
        </div>
      </div>

      {error ? <Callout tone="rework">{`The factory wasn’t created: ${error.replace(/\.$/, '')}.`}</Callout> : null}
    </OnboardingLayout>
  );
}

/** A template's summary and its line as chips: the card's text (template.jpg). */
function TemplateLine({ summary, stations }: { summary: string; stations: PreviewStation[] }) {
  return (
    <span className="flex flex-col gap-2">
      <span className="text-secondary text-ink-soft">{summary}</span>
      {stations.length > 0 ? (
        <span className="flex flex-wrap items-center gap-2">
          {stations.map((s) => (
            <Chip key={s.id} tone={s.kind === 'human' ? 'you' : 'plain'}>
              {s.label}
            </Chip>
          ))}
        </span>
      ) : null}
    </span>
  );
}

/**
 * The created factory's setup, written into its draft: the purpose. A save that fails stays on this step with Retry; the factory is not made a second time.
 */
function SaveSetup({
  org,
  search,
  created,
  preview,
}: {
  org: OrgSummary;
  search: OnboardingSearch;
  created: Created;
  preview: Panel;
}) {
  return (
    <DraftSessionProvider org={org.id} factory={created.factory.id}>
      <SaveDraft org={org} search={search} created={created} preview={preview} />
    </DraftSessionProvider>
  );
}

function SaveDraft({
  org,
  search,
  created,
  preview,
}: {
  org: OrgSummary;
  search: OnboardingSearch;
  created: Created;
  preview: Panel;
}) {
  const session = useDraftSession();
  const navigate = useNavigate();
  const ready = useDraft((s) => s.ready);
  const status = useDraft((s) => s.status);
  const error = useDraft((s) => s.error);
  const dirty = useDraft((s) => s.sections.basics.dirty);
  const [sent, setSent] = useState(false);

  // Once the draft is open, the purpose goes in as one save.
  useEffect(() => {
    if (!ready || sent) return;
    const basics = session.getSnapshot().sections.basics.local;
    session.edit([setBasics(basics, 'purpose', created.purpose)]);
    setSent(true);
  }, [ready, sent, session, created.purpose]);

  // Saved, with nothing waiting in the draft: the Code step can open it.
  useEffect(() => {
    if (!sent || status !== 'saved' || dirty) return;
    void navigate({
      to: '/o/$org/get-started/$step',
      params: { org: org.slug, step: 'code' },
      search: searchOf(search, created.factory.id, created.template),
    });
  }, [sent, status, dirty, navigate, org.slug, search, created.factory.id, created.template]);

  const failed = sent && (error !== null || status === 'failed' || status === 'offline');
  return (
    <OnboardingLayout
      step="factory"
      org={org}
      preview={preview}
      action={
        failed ? (
          <Button type="button" size="lg" variant="primary" onClick={() => session.retryNow()}>
            Retry
          </Button>
        ) : undefined
      }
    >
      <div className="flex flex-col gap-2">
        <h1 className="m-0 text-kpi tracking-display">Start your first factory</h1>
      </div>
      {failed ? (
        <Callout tone="rework">
          {`${created.factory.name} is made, but its setup isn’t saved yet. ${error?.detail ?? 'Retry saves it.'}`}
        </Callout>
      ) : (
        <div aria-busy="true">
          <span role="status" className="sr-only">
            Saving the setup of {created.factory.name}
          </span>
        </div>
      )}
    </OnboardingLayout>
  );
}
