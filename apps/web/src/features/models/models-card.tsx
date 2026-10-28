// "Models agents may use" and "Harness" (canvas "Models and policies", organization/models.html). A switch per
// install model: on means agents may use it, and the default is the model an agent that names none gets.
// Routers and another harness are not built yet, so they render through NotAvailable (ADR 0016, decision 4).
import { Asterisk, Shuffle } from 'lucide-react';
import type { OrgPolicyView } from '../../api/types';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { NotAvailable } from '../../ui/not-available';
import { Switch } from '../../ui/switch';
import { type PolicyForm, toggleModel } from './form';

type Model = OrgPolicyView['models'][number];

/** The line under a model's name: the default's, or who uses it now. */
function usageOf(model: Model, isDefault: boolean): string {
  if (isDefault) return 'All agents unless they choose another';
  if (model.used_by.length === 0) return 'Not used yet';
  return `Used by ${model.used_by.map((u) => `${u.name}'s ${u.agents.join(', ')}`).join('; ')}`;
}

/** An icon tile of a row. The picture's peach tile has no token, so the panel and ink-soft tokens stand in. */
const tile = 'grid size-8 shrink-0 place-items-center rounded-cell bg-panel text-ink-soft';

export function ModelsCard({
  form,
  models,
  onChange,
}: {
  form: PolicyForm;
  models: OrgPolicyView['models'];
  onChange: (next: PolicyForm) => void;
}) {
  const ids = models.map((m) => m.id);
  return (
    <SectionCard title="Models agents may use">
      <ul className="m-0 flex list-none flex-col p-0">
        {models.map((model) => {
          const on = form.allowed_models.includes(model.id);
          const isDefault = form.default_model === model.id;
          // The last model on stays on: a policy needs one model an agent may use.
          const last = on && form.allowed_models.length === 1;
          return (
            <li key={model.id} className="flex items-center gap-3 border-t border-line py-3 first:border-t-0">
              <span className={tile} aria-hidden="true">
                <Asterisk size={17} strokeWidth={1.7} />
              </span>
              <span className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="flex flex-wrap items-center gap-2 font-medium text-ink">
                  {model.label}
                  {isDefault ? <Chip tone="run">Default</Chip> : null}
                </span>
                <span className="text-secondary text-muted">{usageOf(model, isDefault && on)}</span>
              </span>
              <Switch
                checked={on}
                disabled={last}
                aria-label={`Allow ${model.label}`}
                onCheckedChange={() => onChange(toggleModel(form, model.id, ids))}
              />
            </li>
          );
        })}
        <li className="flex items-center gap-3 border-t border-line py-3">
          <span className={cn(tile, 'border border-dashed border-rule text-muted')} aria-hidden="true">
            <Shuffle size={16} strokeWidth={1.6} />
          </span>
          <span className="flex min-w-0 flex-1 flex-col gap-1">
            <span className="font-medium text-ink">Other providers, through a router</span>
            <span className="text-secondary text-muted">Bedrock, Vertex or any OpenAI-compatible endpoint</span>
          </span>
          <NotAvailable label="Add a router">
            <Button size="sm">Add a router</Button>
          </NotAvailable>
        </li>
      </ul>
    </SectionCard>
  );
}

/** One harness option, drawn as a card around a native radio. Only Claude Code is built. */
const harnessCard =
  'flex min-w-0 items-start gap-2 rounded-cell border border-rule p-3 has-checked:border-run has-checked:bg-run-tint';

export function HarnessCard() {
  return (
    <SectionCard
      title="Harness"
      aside={<span className="text-secondary text-muted">The program that runs the model in a box</span>}
    >
      <div role="radiogroup" aria-label="Harness" className="grid gap-2 md:grid-cols-2">
        <label className={harnessCard}>
          <input type="radio" name="harness" aria-label="Claude Code" defaultChecked className="mt-1 accent-run" />
          <HarnessText title="Claude Code" detail="Skills, rules, MCP tools and tool permissions from the definition" />
        </label>
        <label className={cn(harnessCard, 'opacity-60')}>
          <NotAvailable label="Another harness">
            <input type="radio" name="harness" aria-label="Another harness" className="mt-1 accent-run" />
          </NotAvailable>
          <HarnessText title="Another harness" detail="Codex or your own command, per agent" />
        </label>
      </div>
    </SectionCard>
  );
}

function HarnessText({ title, detail }: { title: string; detail: string }) {
  return (
    <span className="flex min-w-0 flex-col gap-1">
      <span className="font-medium text-ink">{title}</span>
      <span className="text-secondary text-ink-soft">{detail}</span>
    </span>
  );
}
