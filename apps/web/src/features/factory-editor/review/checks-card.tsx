// Review's alert and Checks (D §10.10, board Define-Publish): "Fix N problems before you publish
// v9" with a link per error to its section and entity, and B's checks with their problems and
// "Fix it in <Section>". The Evals row is not built yet: it stays visible, disabled, with a tip.
import { Check, Info, TriangleAlert } from 'lucide-react';
import type { CheckResult, DefinitionProblem, Validation } from '../../../api/types';
import type { Model } from '../../../shared/definition/model';
import { plural } from '../../../shared/format';
import { SectionCard } from '../../../ui/card';
import { cn } from '../../../ui/cn';
import { NotAvailable } from '../../../ui/not-available';
import { Skeleton, SkeletonRows } from '../../../ui/skeleton';
import { placeSearch, problemPlace } from '../frame/state';
import { SectionLink } from '../search-link';
import { type EditorSection, routeSection, sectionInfo } from '../sections';

const linkClass = 'underline underline-offset-3 transition-colors duration-(--duration-quick)';

/** A link to where a problem is fixed: its section, and the entity in it. */
function FixLink({
  problem,
  model,
  children,
  className,
}: {
  problem: DefinitionProblem;
  model: Model;
  children: React.ReactNode;
  className?: string;
}) {
  const place = problemPlace(problem, model);
  return (
    <SectionLink
      section={routeSection(place.section as EditorSection)}
      patch={placeSearch(place)}
      className={className}
    >
      {children}
    </SectionLink>
  );
}

const sectionName = (problem: DefinitionProblem, model: Model) =>
  sectionInfo[problemPlace(problem, model).section].label;

export function ProblemAlert({ errors, next, model }: { errors: DefinitionProblem[]; next: number; model: Model }) {
  if (errors.length === 0) return null;
  return (
    <section
      role="alert"
      aria-label={`Fix ${plural(errors.length, 'problem')} before you publish v${next}`}
      className="flex flex-col gap-2 rounded-card border border-rework-tint bg-rework-tint px-4 py-4 md:px-5"
    >
      <p className="m-0 flex items-center gap-2 text-body font-medium text-rework-ink">
        <TriangleAlert size={18} strokeWidth={1.75} aria-hidden="true" />
        Fix {plural(errors.length, 'problem')} before you publish v{next}
      </p>
      <ul className="m-0 flex list-disc flex-col gap-1 pl-9 text-secondary text-rework-ink">
        {errors.map((p) => (
          <li key={`${p.code}${p.pointer ?? ''}${p.message}`}>
            <FixLink problem={p} model={model} className={cn(linkClass, 'text-rework-ink hover:text-rework-ink')}>
              {sectionName(p, model)}: {p.message}
            </FixLink>
          </li>
        ))}
      </ul>
    </section>
  );
}

/** A passed check is a plain green check (build/definition-publish); a warning or failure keeps its icon. */
function CheckIcon({ status }: { status: CheckResult['status'] }) {
  if (status === 'passed')
    return <Check size={16} strokeWidth={1.75} aria-hidden="true" className="mt-px shrink-0 text-ok-ink" />;
  return <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" className="mt-1 shrink-0 text-rework-ink" />;
}

const statusText = { passed: 'Passed', warning: 'Warning', failed: 'Failed' } as const;

export function ChecksCard({
  validation,
  model,
  failed = false,
}: {
  validation: Validation | undefined;
  model: Model;
  /** The checks could not be read: a plain line replaces the skeleton. */
  failed?: boolean;
}) {
  const checks = validation?.checks ?? [];
  const passed = checks.filter((c) => c.status === 'passed').length;
  const problems = validation?.problems.filter((p) => p.severity !== 'info').length ?? 0;
  return (
    <SectionCard
      title="Checks"
      aside={
        validation ? (
          <span className="pt-1 text-secondary text-muted">
            {passed} passed{problems > 0 ? `, ${plural(problems, 'problem')}` : ''}
          </span>
        ) : null
      }
    >
      {!validation && failed ? (
        <p className="m-0 text-body text-muted">The checks could not be read. Reload the page to try again.</p>
      ) : !validation ? (
        <Skeleton ready={false} placeholder={<SkeletonRows rows={4} />}>
          {null}
        </Skeleton>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {checks.map((c, i) => {
            const own = validation.problems.filter((p) => p.check === c.id && p.severity !== 'info');
            return (
              <li key={c.id} className={cn('flex gap-3 py-3 text-body', i > 0 && 'border-t border-line')}>
                <CheckIcon status={c.status} />
                <div className="flex min-w-0 flex-col gap-2">
                  <span>
                    <span className="sr-only">{statusText[c.status]}: </span>
                    {c.status === 'passed' || own.length === 0 ? c.summary : null}
                  </span>
                  {own.map((p) => (
                    <p key={`${p.code}${p.pointer ?? ''}${p.message}`} className="m-0">
                      <span className="text-rework-ink">{p.message}.</span>{' '}
                      <FixLink problem={p} model={model} className={cn(linkClass, 'hover:text-link')}>
                        Fix it in {sectionName(p, model)}
                      </FixLink>
                    </p>
                  ))}
                </div>
              </li>
            );
          })}
          {validation.problems
            .filter((p) => p.severity === 'info')
            .map((p) => (
              <li key={`info${p.code}${p.message}`} className="flex gap-3 border-t border-line py-3 text-body">
                <Info size={16} strokeWidth={1.75} aria-hidden="true" className="mt-1 shrink-0 text-link" />
                <span>{p.message}</span>
              </li>
            ))}
          <li className="border-t border-line py-3">
            <NotAvailable label="Evals before publish">
              <button type="button" className="flex w-full items-center gap-3 text-left text-body text-muted">
                <Check size={16} strokeWidth={1.75} aria-hidden="true" className="shrink-0" />
                Evals before publish
              </button>
            </NotAvailable>
          </li>
        </ul>
      )}
    </SectionCard>
  );
}
