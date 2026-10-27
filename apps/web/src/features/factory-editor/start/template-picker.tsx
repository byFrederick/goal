// From a template (D §10.11, canvas New-Start): the catalog's templates as radio rows ("5 in the
// bucket", "6 stations, 3 agents") and the chosen one's detail: its version and day, the line as
// chips (stations where a person decides in violet), what its agents do and what it connects to.
// The blank template is the Blank card's, so it is not listed here.
import { useQuery } from '@tanstack/react-query';
import { ArrowRight, LayoutTemplate } from 'lucide-react';
import { useId } from 'react';
import { templateQuery, templatesQuery } from '../../../api/queries';
import type { TemplateSummary } from '../../../api/types';
import { modelOf } from '../../../shared/definition/model';
import { formatDay, plural } from '../../../shared/format';
import { useOrg } from '../../../shared/org';
import { Chip } from '../../../ui/chip';
import { cn } from '../../../ui/cn';
import { EmptyState } from '../../../ui/empty-state';
import { RadioList } from '../../../ui/radio-cards';
import { agentsSentence, blankTemplate, lineChips } from './start-choice';

const card = 'flex min-w-0 flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5';

/** The catalog's templates but the blank one, by title. */
export function useTemplates() {
  const org = useOrg();
  const q = useQuery(templatesQuery(org.id));
  const templates = (q.data ?? [])
    .filter((t) => t.name !== blankTemplate)
    .toSorted((a, b) => a.title.localeCompare(b.title));
  return { ...q, templates, blank: q.data?.find((t) => t.name === blankTemplate) };
}

export function TemplatePicker({ value, onChange }: { value: string | undefined; onChange: (name: string) => void }) {
  const { templates, isPending, error } = useTemplates();
  const headingId = useId();

  if (isPending) return <div aria-busy="true" className={cn(card, 'h-64')} />;
  if (error) {
    return (
      <p role="alert" className="m-0 rounded-control border border-rework-tint bg-rework-tint px-4 py-3 text-sm">
        The templates can’t be read right now: goald is not answering. Try again in a moment.
      </p>
    );
  }
  if (templates.length === 0) {
    return (
      <EmptyState icon={<LayoutTemplate size={20} strokeWidth={1.5} />} title="No templates in the bucket yet">
        Templates are factories saved as data. An operator adds one with{' '}
        <code className="font-mono text-[13px] text-ink">goal catalog templates push &lt;dir&gt;</code>; until then,
        start blank or clone a factory.
      </EmptyState>
    );
  }

  return (
    <div className="grid items-start gap-3 md:gap-4 @[960px]:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
      <section aria-labelledby={headingId} className={card}>
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h2 id={headingId} className="m-0 text-xl font-medium tracking-[-0.01em]">
            Templates
          </h2>
          <span className="text-[13px] text-muted">{templates.length} in the bucket</span>
        </div>
        <RadioList
          legend="Template"
          name="template"
          value={value}
          onChange={onChange}
          options={templates.map((t) => ({
            value: t.name,
            label: t.title,
            description: t.summary,
            aside: `${plural(t.stations, 'station')}, ${plural(t.agents, 'agent')}`,
          }))}
        />
      </section>
      {value ? <TemplateDetail summary={templates.find((t) => t.name === value)} name={value} /> : null}
    </div>
  );
}

function TemplateDetail({ name, summary }: { name: string; summary: TemplateSummary | undefined }) {
  const org = useOrg();
  const { data: template, error } = useQuery(templateQuery(org.id, name));
  const headingId = useId();
  const model = template ? modelOf(template.model) : undefined;
  const chips = lineChips(model?.line.stations ?? []);
  const connectors = template?.connectors ?? summary?.connectors ?? [];
  const label = 'm-0 text-sm font-medium';

  return (
    <section aria-labelledby={headingId} aria-busy={!template && !error} className={card}>
      <div>
        <h2 id={headingId} className="m-0 text-xl font-medium tracking-[-0.01em]">
          {template?.title ?? summary?.title ?? name}
        </h2>
        {summary ? (
          <p className="m-0 mt-1 text-sm text-muted">
            Template v{summary.version}, last changed {formatDay(summary.changed)}
          </p>
        ) : null}
      </div>
      {error ? (
        <p className="m-0 text-sm text-rework-ink">This template can’t be read right now. Try again in a moment.</p>
      ) : null}
      {template?.description ? <p className="m-0 text-sm leading-normal">{template.description}</p> : null}
      <div className="flex flex-col gap-2">
        <h3 className={label}>The line</h3>
        <ol
          aria-label={`The line of ${template?.title ?? name}`}
          className="m-0 flex list-none flex-wrap items-center gap-1.5 p-0"
        >
          {chips.map((c, i) => (
            <li key={c.id} className="flex items-center gap-1.5">
              <Chip tone={c.human ? 'you' : 'plain'} className="h-8 px-2.5 text-[13px] font-normal">
                {c.label}
                {c.human ? <span className="sr-only">, a person decides</span> : null}
              </Chip>
              {i < chips.length - 1 ? (
                <ArrowRight size={14} strokeWidth={1.5} aria-hidden="true" className="text-muted" />
              ) : null}
            </li>
          ))}
        </ol>
      </div>
      <div className="flex flex-col gap-1.5">
        <h3 className={label}>Agents</h3>
        <p className="m-0 text-[15px] leading-normal">{model ? agentsSentence(model.agents.agents ?? []) : '…'}</p>
      </div>
      <div className="flex flex-col gap-2">
        <h3 className={label}>Connects to</h3>
        {connectors.length > 0 ? (
          <ul className="m-0 flex list-none flex-wrap gap-1.5 p-0">
            {connectors.map((c) => (
              <li key={c}>
                <Chip>{c}</Chip>
              </li>
            ))}
          </ul>
        ) : (
          <p className="m-0 text-sm text-muted">Nothing outside the workspace.</p>
        )}
        <p className="m-0 text-[13px] text-muted">Templates never include secrets.</p>
      </div>
    </section>
  );
}
