// Review's alert and Checks (D §10.10, board Define-Publish): "Fix N problems before you publish
// v9" with a link per error to its section and entity, and B's checks with their problems and
// "Fix it in <Section>".
import { Check, Info, TriangleAlert } from 'lucide-react';
import type { CheckResult, DefinitionProblem, Validation } from '../../../api/types';
import type { Model } from '../../../shared/definition/model';
import { plural } from '../../../shared/format';
import { SectionCard } from '../../../ui/card';
import { cn } from '../../../ui/cn';
import { placeSearch, problemPlace } from '../frame/state';
import { SectionLink } from '../search-link';
import { type EditorSection, sectionInfo } from '../sections';

const linkClass = 'underline underline-offset-3 transition-colors duration-200';

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
    <SectionLink section={place.section as EditorSection} patch={placeSearch(place)} className={className}>
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
      className="flex flex-col gap-2 rounded-card border border-rework-line bg-rework-tint px-4 py-3.5 md:px-5"
    >
      <p className="m-0 flex items-center gap-2 text-[15px] font-medium text-rework-ink">
        <TriangleAlert size={18} strokeWidth={1.75} aria-hidden="true" />
        Fix {plural(errors.length, 'problem')} before you publish v{next}
      </p>
      <ul className="m-0 flex list-disc flex-col gap-1 pl-9 text-sm text-rework-ink">
        {errors.map((p) => (
          <li key={`${p.code}${p.pointer ?? ''}${p.message}`}>
            <FixLink problem={p} model={model} className={cn(linkClass, 'text-rework-ink hover:text-rework')}>
              {sectionName(p, model)}: {p.message}
            </FixLink>
          </li>
        ))}
      </ul>
    </section>
  );
}

function CheckIcon({ status }: { status: CheckResult['status'] }) {
  if (status === 'passed')
    return <Check size={16} strokeWidth={2} aria-hidden="true" className="mt-0.5 shrink-0 text-ok" />;
  return <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" className="mt-0.5 shrink-0 text-rework" />;
}

const statusText = { passed: 'Passed', warning: 'Warning', failed: 'Failed' } as const;

export function ChecksCard({ validation, model }: { validation: Validation | undefined; model: Model }) {
  const checks = validation?.checks ?? [];
  const passed = checks.filter((c) => c.status === 'passed').length;
  const problems = validation?.problems.filter((p) => p.severity !== 'info').length ?? 0;
  return (
    <SectionCard
      title="Checks"
      aside={
        validation ? (
          <span className="pt-1 text-sm text-muted">
            {passed} passed{problems > 0 ? `, ${plural(problems, 'problem')}` : ''}
          </span>
        ) : null
      }
    >
      {!validation ? (
        <div aria-busy="true" className="h-32 rounded-control bg-control/60">
          <span className="sr-only">Checking the draft</span>
        </div>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {checks.map((c, i) => {
            const own = validation.problems.filter((p) => p.check === c.id && p.severity !== 'info');
            return (
              <li
                key={c.id}
                className={cn('flex gap-3 py-2.5 text-[15px] leading-normal', i > 0 && 'border-t border-line')}
              >
                <CheckIcon status={c.status} />
                <div className="flex min-w-0 flex-col gap-1.5">
                  <span>
                    <span className="sr-only">{statusText[c.status]}: </span>
                    {c.status === 'passed' || own.length === 0 ? c.summary : null}
                  </span>
                  {own.map((p) => (
                    <p key={`${p.code}${p.pointer ?? ''}${p.message}`} className="m-0">
                      <span className="text-rework-ink">{p.message}.</span>{' '}
                      <FixLink problem={p} model={model} className={cn(linkClass, 'hover:text-run')}>
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
              <li key={`info${p.code}${p.message}`} className="flex gap-3 border-t border-line py-2.5 text-[15px]">
                <Info size={16} strokeWidth={1.75} aria-hidden="true" className="mt-0.5 shrink-0 text-run" />
                <span>{p.message}</span>
              </li>
            ))}
        </ul>
      )}
    </SectionCard>
  );
}
