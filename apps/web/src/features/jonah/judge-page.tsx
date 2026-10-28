// Jonah's judge (ADR 0025, the Jonah screens in ADR 0016): one judge of the current version, read and edited
// in one place. Its file is the form: what it checks, the agents it scores, the rubric, the outcomes and
// their scores, the pass mark, the sample, the model and the switch. Save changes publishes the file as a
// new version, and the page shows a refusal where it happened. Try on 10 recent runs queues the judge on
// the newest finished runs of its agents; those scores never count in the 30 days. Nothing polls.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Play, Plus, X } from 'lucide-react';
import { type ChangeEvent, type FormEvent, useState } from 'react';
import { ApiError, tryJudge, updateJudge } from '../../api/client';
import { builderMetaQuery, judgeQuery, versionModelQuery } from '../../api/queries';
import type { Judge, JudgeScore, JudgeSummary } from '../../api/types';
import { TopBarSlot } from '../../app/top-bar';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { formatUsd } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { SectionCard } from '../../ui/card';
import { Chip, type ChipTone } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { EmptyState } from '../../ui/empty-state';
import { Field, Input, useFieldControl } from '../../ui/field';
import { Switch } from '../../ui/switch';
import { formErrors, formOf, type JudgeForm, type JudgeFormErrors, judgeWrite } from './judge-form';
import { RangeInput } from './judge-range';
import { refreshAfterPublish } from './judges-cache';
import {
  itemNumber,
  none,
  outcomePasses,
  passMarkText,
  publishFailure,
  sampleText,
  scoreText,
  seesText,
  summaryLine,
  tryFailure,
} from './judges-format';

/** Loading shows the app background and an empty region for the screen reader, never a placeholder shape. */
function Loading() {
  return (
    <div aria-busy="true" className="min-h-40">
      <p className="sr-only">Loading</p>
    </div>
  );
}

/** A score's chip: green when it passed, red when it failed, plain while it is still on its way. */
function scoreTone(score: JudgeScore): ChipTone {
  if (score.state === 'error') return 'rework';
  if (score.state !== 'scored') return 'plain';
  return score.passed ? 'ok' : 'rework';
}

/**
 * What the sample costs in words: the runs a week and the cost of each, from what the judge scored in the
 * last 30 days. At 0% the judge pauses, and the hint says so alone.
 */
function sampleHint(summary: JudgeSummary, savedSample: number, sample: number): string {
  if (sample === 0) return '0% pauses it.';
  if (summary.scored === 0 || savedSample === 0) return '0% pauses it.';
  const perWeek = Math.round(((summary.scored * 7) / 30) * (sample / savedSample));
  const each = formatUsd(summary.cost_usd / summary.scored);
  return `About ${perWeek} runs a week, ${each} each. 0% pauses it.`;
}

/** The judge's file as a form, and the page's actions over it. Remounted when a new version arrives. */
function JudgeEditor({
  judge,
  canPublish,
}: {
  judge: Judge;
  /** definition:publish: saving and trying publish or queue, so only those who may see them act. */
  canPublish: boolean;
}) {
  const factory = useFactory();
  const org = useOrg();
  const queryClient = useQueryClient();
  const models = useQuery(builderMetaQuery(org.id));
  const version = factory.current?.id;
  // The agents to add come from the version the judge is read from: the factory's model names them.
  const current = useQuery({ ...versionModelQuery(org.id, factory.id, version ?? ''), enabled: version !== undefined });
  const [form, setForm] = useState<JudgeForm>(() => formOf(judge));
  const [submitted, setSubmitted] = useState(false);
  const [addingAgent, setAddingAgent] = useState(false);

  const errors: JudgeFormErrors = formErrors(form);
  const shownErrors: JudgeFormErrors = submitted ? errors : {};
  const set = (patch: Partial<JudgeForm>) => {
    if (save.error) save.reset();
    setForm((f) => ({ ...f, ...patch }));
  };

  const save = useMutation({
    mutationFn: () => updateJudge(org.id, factory.id, judge.slug, judgeWrite(judge.title, form, version)),
    onSuccess: () => refreshAfterPublish(queryClient, org.id, factory.id),
  });

  const trying = useMutation({
    mutationFn: () => tryJudge(org.id, factory.id, judge.slug),
    onSuccess: () =>
      void queryClient.invalidateQueries({ queryKey: judgeQuery(org.id, factory.id, judge.slug).queryKey }),
  });

  const agentNames = (current.data?.model.agents.agents ?? []).flatMap((a) =>
    typeof a.name === 'string' ? [a.name] : [],
  );
  const addable = agentNames.filter((name) => !form.agents.includes(name));

  // The model select: the install's models, with the judge's own model kept when it is not among them.
  const modelChoices = models.data?.models ?? [];
  const modelOptions =
    form.model !== '' && !modelChoices.some((m) => m.id === form.model)
      ? [...modelChoices, { id: form.model, label: form.model, default: false }]
      : modelChoices;

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    setSubmitted(true);
    if (Object.keys(errors).length > 0) return;
    if (!save.isPending) save.mutate();
  };

  const setOutcome = (index: number, patch: Partial<JudgeForm['outcomes'][number]>) =>
    set({ outcomes: form.outcomes.map((o, i) => (i === index ? { ...o, ...patch } : o)) });

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <TopBarSlot
        crumb={judge.title}
        actions={[
          <Button
            key="try"
            variant="secondary"
            disabled={!canPublish || trying.isPending}
            onClick={() => trying.mutate()}
          >
            <Play aria-hidden="true" />
            Try on 10 recent runs
          </Button>,
          <Button key="save" variant="primary" type="submit" form="judge-form" disabled={!canPublish || save.isPending}>
            Save changes
          </Button>,
        ]}
      />

      <div className="flex flex-wrap items-center gap-4">
        <h1 className="m-0 text-page-title tracking-display">{judge.title}</h1>
        <span className="inline-flex items-center gap-2 font-control text-secondary font-semibold">
          <Switch
            aria-label={`${judge.title} on`}
            checked={form.enabled}
            disabled={!canPublish}
            onCheckedChange={(enabled) => set({ enabled })}
          />
          On
        </span>
        <span className="text-secondary text-muted md:ml-auto">{summaryLine(judge.summary)}</span>
      </div>

      {save.isError ? (
        <div aria-live="polite">
          <Callout tone="rework">{publishFailure(save.error)}</Callout>
        </div>
      ) : null}
      {trying.isError ? (
        <div aria-live="polite">
          <Callout tone="rework">{tryFailure(trying.error)}</Callout>
        </div>
      ) : null}
      {trying.isSuccess ? (
        <div aria-live="polite">
          <Callout tone="note">Queued on the 10 newest runs. Their scores appear under Recent scores.</Callout>
        </div>
      ) : null}

      <div className="flex flex-col gap-5 lg:flex-row lg:items-start">
        <form id="judge-form" onSubmit={onSubmit} noValidate className="flex min-w-0 flex-1 flex-col gap-5">
          <fieldset disabled={!canPublish} className="contents">
            <Field label="What it checks" error={shownErrors.criterion}>
              <Input value={form.criterion} onChange={(e) => set({ criterion: e.target.value })} maxLength={200} />
            </Field>

            <fieldset className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
              <legend className="mb-2 text-sm font-medium text-ink">Agents it scores</legend>
              <div className="flex flex-wrap items-center gap-2">
                {form.agents.length === 0 ? <span className="text-secondary text-ink-soft">All agents</span> : null}
                {form.agents.map((agent) => (
                  <Chip key={agent} size="lg">
                    {agent}
                    <button
                      type="button"
                      aria-label={`Remove ${agent}`}
                      onClick={() => set({ agents: form.agents.filter((a) => a !== agent) })}
                      className="grid size-5 place-items-center rounded-chip text-muted hover:text-ink"
                    >
                      <X aria-hidden="true" size={14} />
                    </button>
                  </Chip>
                ))}
                {addingAgent ? (
                  <select
                    aria-label="Agent to add"
                    ref={(el) => el?.focus()}
                    className="h-10 rounded-control border border-rule bg-card px-2 text-secondary text-ink"
                    onChange={(e: ChangeEvent<HTMLSelectElement>) => {
                      if (e.target.value !== '') set({ agents: [...form.agents, e.target.value] });
                      setAddingAgent(false);
                    }}
                    onBlur={() => setAddingAgent(false)}
                    defaultValue=""
                  >
                    <option value="">Choose an agent</option>
                    {addable.map((name) => (
                      <option key={name} value={name}>
                        {name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={addable.length === 0}
                    onClick={() => setAddingAgent(true)}
                  >
                    <Plus aria-hidden="true" />
                    Add an agent
                  </Button>
                )}
              </div>
            </fieldset>

            <Field
              label="Rubric"
              aside={<span className="font-mono text-meta text-muted">{`evals/judges/${judge.slug}.md`}</span>}
            >
              <RubricArea value={form.rubric} onChange={(rubric) => set({ rubric })} />
            </Field>

            <fieldset className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
              <legend className="mb-2 text-sm font-medium text-ink">Outcomes</legend>
              {form.outcomes.map((outcome, i) => (
                // biome-ignore lint/suspicious/noArrayIndexKey: outcomes have no id; a row is kept by its place
                <div key={i} className="flex flex-wrap items-center gap-2 rounded-card border border-line bg-card p-3">
                  <Input
                    type="number"
                    min={0}
                    max={1}
                    step={0.05}
                    aria-label={`Outcome ${i + 1} score`}
                    value={outcome.score}
                    onChange={(e) => setOutcome(i, { score: Number(e.target.value) })}
                    className="w-20 font-numeral"
                  />
                  <Input
                    aria-label={`Outcome ${i + 1} label`}
                    value={outcome.label}
                    onChange={(e) => setOutcome(i, { label: e.target.value })}
                    className="w-40 font-control font-semibold"
                  />
                  <Input
                    aria-label={`Outcome ${i + 1} meaning`}
                    value={outcome.meaning}
                    onChange={(e) => setOutcome(i, { meaning: e.target.value })}
                    className="min-w-48 flex-1"
                  />
                  <Chip tone={outcomePasses(outcome, form.passMark) ? 'ok' : 'rework'}>
                    {outcomePasses(outcome, form.passMark) ? 'Passes' : 'Fails'}
                  </Chip>
                  <Button
                    size="sm"
                    variant="danger"
                    aria-label={`Remove outcome ${i + 1}`}
                    onClick={() => set({ outcomes: form.outcomes.filter((_, j) => j !== i) })}
                  >
                    Remove
                  </Button>
                </div>
              ))}
              {shownErrors.outcomes ? <p className="m-0 text-meta text-rework-ink">{shownErrors.outcomes}</p> : null}
              <Button
                size="sm"
                variant="secondary"
                className="self-start"
                onClick={() => set({ outcomes: [...form.outcomes, { score: 0, label: '', meaning: '' }] })}
              >
                <Plus aria-hidden="true" />
                Add an outcome
              </Button>
            </fieldset>

            <div className="grid gap-5 sm:grid-cols-2">
              <Field label="Pass mark" help="Outcomes scored below it fail and go to Jonah.">
                <RangeInput
                  label="Pass mark"
                  min={0}
                  max={1}
                  step={0.05}
                  value={form.passMark}
                  shown={passMarkText(form.passMark)}
                  onChange={(passMark) => set({ passMark })}
                />
              </Field>
              <Field label="Sample" help={sampleHint(judge.summary, judge.sample, form.sample)}>
                <RangeInput
                  label="Sample"
                  min={0}
                  max={100}
                  step={5}
                  value={Math.round(form.sample * 100)}
                  shown={sampleText(form.sample)}
                  onChange={(percent) => set({ sample: percent / 100 })}
                />
              </Field>
            </div>

            <Field label="Judge model" help="Empty, the factory's runtime model runs the judge.">
              <ModelSelect value={form.model} options={modelOptions} onChange={(model) => set({ model })} />
            </Field>
          </fieldset>
        </form>

        <aside className="flex min-w-0 flex-col gap-4 lg:w-85 lg:shrink-0">
          <SectionCard title="Recent scores" headingLevel="h2">
            {judge.recent.length === 0 ? (
              <p className="m-0 text-secondary text-muted">No scores yet.</p>
            ) : (
              <ul className="m-0 flex list-none flex-col p-0">
                {judge.recent.map((score) => (
                  <li key={score.id} className="flex flex-col gap-1 border-b border-line py-3 last:border-b-0">
                    <span className="font-control text-secondary font-semibold">
                      {itemNumber(score.item_ref)} {score.item_title}
                    </span>
                    <span>
                      <Chip tone={scoreTone(score)}>
                        {score.state === 'scored' && score.score !== undefined
                          ? `${scoreText(score)}, ${score.score}`
                          : scoreText(score)}
                      </Chip>
                    </span>
                    {score.reason ? <span className="text-meta text-muted">{score.reason}</span> : null}
                  </li>
                ))}
              </ul>
            )}
          </SectionCard>

          <SectionCard title="Runs on" headingLevel="h2">
            <dl className="m-0 flex flex-col gap-3 text-secondary">
              <RunsOnRow term="Box" value={judge.runs_on.size} />
              <RunsOnRow
                term="Connections"
                value={judge.runs_on.connections.length === 0 ? none : judge.runs_on.connections.join(', ')}
              />
              <RunsOnRow term="Sees" value={seesText(judge.runs_on.sees)} />
            </dl>
            {judge.runs_on.inherited ? (
              <p className="m-0 text-meta text-muted">Inherited from the factory's defaults</p>
            ) : null}
          </SectionCard>
        </aside>
      </div>
    </div>
  );
}

function RunsOnRow({ term, value }: { term: string; value: string }) {
  return (
    <div className="flex flex-col gap-1 sm:flex-row sm:gap-3">
      <dt className="w-28 shrink-0 text-muted">{term}</dt>
      <dd className="m-0 min-w-0">{value}</dd>
    </div>
  );
}

/** The rubric: Markdown text in the card's text face, with the Field's id and help. */
function RubricArea({ value, onChange }: { value: string; onChange: (rubric: string) => void }) {
  const field = useFieldControl();
  return (
    <textarea
      id={field?.id}
      aria-describedby={field?.describedBy}
      rows={12}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="w-full min-w-0 rounded-control border border-rule bg-card p-3 text-body text-ink hover:border-muted"
    />
  );
}

/** The model select: the factory's default is the empty choice, which the contract sends as no model. */
function ModelSelect({
  value,
  options,
  onChange,
}: {
  value: string;
  options: readonly { id: string; label: string }[];
  onChange: (model: string) => void;
}) {
  const field = useFieldControl();
  return (
    <select
      id={field?.id}
      aria-describedby={field?.describedBy}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className={cn(
        'h-11 w-full max-w-sm min-w-0 rounded-control border border-rule bg-card px-3 text-base text-ink md:h-10',
        'hover:border-muted',
      )}
    >
      <option value="">Factory default</option>
      {options.map((m) => (
        <option key={m.id} value={m.id}>
          {m.label}
        </option>
      ))}
    </select>
  );
}

/** One judge's page. Its own four states: loading, a judge that is not there or cannot be read, and ready. */
export function JudgePage({ slug }: { slug: string }) {
  const factory = useFactory();
  const org = useOrg();
  const params = useFactoryParams();
  const judge = useQuery(judgeQuery(org.id, factory.id, slug));
  const canPublish = factory.can.includes('definition:publish');

  if (judge.isPending) return <Loading />;

  if (judge.isError) {
    const missing = judge.error instanceof ApiError && judge.error.code === 'judge-not-found';
    return (
      <div className="flex min-w-0 flex-col gap-5">
        <TopBarSlot crumb="Judges" />
        <EmptyState
          title={missing ? 'This judge is not in the current version' : 'The judge could not be loaded'}
          actions={
            missing ? (
              <Button asChild variant="secondary">
                <Link to="/o/$org/f/$factory/jonah/judges" params={params}>
                  Back to judges
                </Link>
              </Button>
            ) : (
              <Button variant="secondary" onClick={() => void judge.refetch()}>
                Try again
              </Button>
            )
          }
        >
          {missing
            ? "It is not in the factory's current version, so it scores nothing."
            : 'goald is not answering, or judges are not available on it. Try again in a moment.'}
        </EmptyState>
      </div>
    );
  }

  return <JudgeEditor key={judge.data.version} judge={judge.data} canPublish={canPublish} />;
}
