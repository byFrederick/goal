// Checks on <environment>: the draft validation's problems for this environment's file, each with its
// quick fixes, then what is fine (the secrets exist, the servers answered). A fix patches the file's
// spec (applyPatch); Move to Secrets first creates the secret in place from the value in the file.
import { useQuery } from '@tanstack/react-query';
import { Check, TriangleAlert } from 'lucide-react';
import { type ReactNode, useState } from 'react';
import { factoriesQuery } from '../../../api/queries';
import type { DefinitionProblem, ProblemFix, ServerTest } from '../../../api/types';
import { plural } from '../../../shared/format';
import { useOrg } from '../../../shared/org';
import { Button } from '../../../ui/button';
import { cn } from '../../../ui/cn';
import { NewSecretSheet } from '../../secrets/new-secret-sheet';
import { useValidation } from '../frame/use-editor';
import { applyPatch, type EnvSpec, envPath, patchTarget, secretRef, secretsUsed } from './env-file';
import { Card, rowPad, useSecretOptions } from './parts';

/** The draft validation's problems that are in this environment's file. */
export function useEnvironmentProblems(environment: string): DefinitionProblem[] {
  const problems = useValidation()?.problems ?? [];
  const path = envPath(environment);
  return problems.filter(
    (p) => p.section === 'environments' && (p.file === path || p.pointer?.startsWith(`/environments/${environment}`)),
  );
}

/** The plain value a Move to Secrets fix moves: the file's value at the patch's target. */
function movedValue(spec: EnvSpec, fix: ProblemFix): string {
  const [kind, key] = patchTarget(fix.patch[0] ?? { op: 'replace', path: '' });
  if (kind === 'env' && key) return spec.env[key] ?? '';
  return '';
}

/** Applies fixes; Move to Secrets (a replace by a `{{name}}` of a secret that may not exist yet) opens the New secret sheet. */
export function useFixes({ spec, change }: { spec: EnvSpec; change: (edit: (text: string) => string) => void }) {
  const org = useOrg();
  const factories = useQuery(factoriesQuery(org.id)).data ?? [];
  const secrets = useSecretOptions();
  const [moving, setMoving] = useState<ProblemFix | null>(null);
  const apply = (fix: ProblemFix) => {
    const op = fix.patch[0];
    const name = typeof op?.value === 'string' ? secretRef(op.value) : undefined;
    const exists = name !== undefined && secrets.options.some((o) => o.value === name);
    if (op && name !== undefined && !exists) setMoving(fix);
    else change((t) => applyPatch(t, fix.patch));
  };
  const name =
    moving?.patch[0] && typeof moving.patch[0].value === 'string' ? secretRef(moving.patch[0].value) : undefined;
  const sheet = (
    <NewSecretSheet
      org={org.id}
      open={moving !== null}
      onOpenChange={(open) => !open && setMoving(null)}
      factories={factories}
      initial={{ ...(name ? { name } : {}), value: moving ? movedValue(spec, moving) : '' }}
      onCreated={() => {
        if (moving) change((t) => applyPatch(t, moving.patch));
        setMoving(null);
      }}
    />
  );
  return { apply, sheet };
}

function FixButtons({
  fixes,
  apply,
  disabled,
}: {
  fixes: ProblemFix[];
  apply: (fix: ProblemFix) => void;
  disabled: boolean;
}) {
  if (disabled || fixes.length === 0) return null;
  return (
    <span className="flex flex-wrap gap-2">
      {fixes.map((f) => (
        <Button key={f.label} variant="white" size="sm" onClick={() => apply(f)}>
          {f.label}
        </Button>
      ))}
    </span>
  );
}

/** The plain-key problems under the variables: a tinted bar with its fix, as in the picture. */
export function InlineProblems({
  environment,
  apply,
  readOnly,
}: {
  environment: string;
  apply: (fix: ProblemFix) => void;
  readOnly: boolean;
}) {
  const problems = useEnvironmentProblems(environment).filter((p) => p.code === 'plain-key' && p.fixes.length > 0);
  if (problems.length === 0) return null;
  return (
    <ul className={cn('m-0 flex list-none flex-col gap-2 pb-4 pt-0 p-0', rowPad)}>
      {problems.map((p) => (
        <li
          key={`${p.pointer}${p.message}`}
          className="flex flex-col gap-2 rounded-control bg-rework-tint px-3 py-3 text-secondary text-rework-ink md:flex-row md:items-center md:justify-between"
        >
          <span className="flex items-start gap-2">
            <TriangleAlert size={14} strokeWidth={1.75} aria-hidden="true" className="mt-1 shrink-0" />
            <span>{p.message}</span>
          </span>
          <FixButtons fixes={p.fixes} apply={apply} disabled={readOnly} />
        </li>
      ))}
    </ul>
  );
}

function Row({ icon, children, action }: { icon: ReactNode; children: ReactNode; action?: ReactNode }) {
  return (
    <li
      className={cn(
        'flex flex-col gap-2 border-b border-line py-3 last:border-b-0 md:flex-row md:items-center md:justify-between',
        rowPad,
      )}
    >
      <span className="flex items-start gap-3 text-body">
        {icon}
        <span>{children}</span>
      </span>
      {action}
    </li>
  );
}

const bad = <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" className="mt-1 shrink-0 text-rework-ink" />;
const good = <Check size={16} strokeWidth={1.75} aria-hidden="true" className="mt-1 shrink-0 text-ok-ink" />;

export function ChecksCard({
  environment,
  spec,
  apply,
  readOnly,
  results,
  factoryName,
  testAll,
  testing,
}: {
  environment: string;
  spec: EnvSpec;
  apply: (fix: ProblemFix) => void;
  readOnly: boolean;
  results: Record<string, ServerTest>;
  factoryName: string;
  testAll: () => void;
  testing: boolean;
}) {
  const problems = useEnvironmentProblems(environment);
  const { options, ready } = useSecretOptions();
  const used = secretsUsed(spec);
  const missing = used.filter((n) => !options.some((o) => o.value === n));
  const remote = Object.entries(spec.mcp).filter(([, s]) => s.url);
  const answered = remote.filter(([n]) => results[n]?.ok).length;
  const tested = remote.some(([n]) => results[n]);
  return (
    <Card
      title={`Checks on ${environment}`}
      aside={<span className="text-secondary text-muted">The same checks as publish</span>}
    >
      <ul className="m-0 flex list-none flex-col p-0">
        {problems.map((p) => (
          <Row
            key={`${p.code}${p.pointer}${p.message}`}
            icon={bad}
            action={<FixButtons fixes={p.fixes} apply={apply} disabled={readOnly} />}
          >
            {p.message}
          </Row>
        ))}
        {ready && used.length > 0 ? (
          missing.length === 0 ? (
            <Row icon={good}>
              {plural(used.length, 'secret')} exist{used.length === 1 ? 's' : ''} and {used.length === 1 ? 'is' : 'are'}{' '}
              open to {factoryName}.
            </Row>
          ) : (
            <Row icon={bad}>
              {missing.join(', ')} {missing.length === 1 ? 'is' : 'are'} not in Secrets.
            </Row>
          )
        ) : null}
        {remote.length > 0 ? (
          <Row
            icon={
              tested && answered === remote.length ? (
                good
              ) : tested ? (
                bad
              ) : (
                <span aria-hidden="true" className="size-4 shrink-0" />
              )
            }
            action={
              <Button variant="white" size="sm" onClick={testAll} disabled={testing}>
                {tested ? 'Test again' : 'Test servers'}
              </Button>
            }
          >
            {tested
              ? `${answered} of ${plural(remote.length, 'MCP server')} answered.`
              : `${plural(remote.length, 'MCP server')} not tested yet.`}
          </Row>
        ) : null}
        {problems.length === 0 && used.length === 0 && remote.length === 0 ? (
          <Row icon={good}>Nothing to check yet.</Row>
        ) : null}
      </ul>
    </Card>
  );
}
