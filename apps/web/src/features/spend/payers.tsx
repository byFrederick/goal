// Who pays for models: each model connection the factories' runs use, Connected, and two ways to add
// another. "Add a key" goes to adding a connection, where a key is kept today. The router row is not
// built yet (ADR 0016, decision 4): its Set up is disabled with the tooltip "Not available yet".
import { Link } from '@tanstack/react-router';
import { KeyRound, Route } from 'lucide-react';
import type { ReactNode } from 'react';
import type { SpendBudget } from '../../api/types';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { NotAvailable } from '../../ui/not-available';
import { connectionsOf, usedByText } from './view';

export function Payers({ budgets, org, total }: { budgets: readonly SpendBudget[]; org: string; total: number }) {
  const connections = connectionsOf(budgets);
  return (
    <section aria-labelledby="acc-h" className="flex min-w-0 flex-col rounded-card border border-line bg-card p-4">
      <h2 id="acc-h" className="m-0 mb-2 text-heading text-ink">
        Who pays for models
      </h2>
      <ul className="m-0 flex list-none flex-col p-0">
        {connections.map((c) => (
          <li key={c.name} className="flex items-center gap-3 border-t border-line py-3">
            <Icon>
              <KeyRound size={17} strokeWidth={1.6} aria-hidden="true" />
            </Icon>
            <span className="flex min-w-0 flex-1 flex-col leading-snug">
              <b className="font-semibold text-ink">{c.name}</b>
              <span className="text-secondary text-muted">
                Connection {c.name}, {usedByText(c.used, total)}
              </span>
            </span>
            <Chip tone="ok" dot>
              Connected
            </Chip>
          </li>
        ))}
        <li className="flex items-center gap-3 border-t border-line py-3">
          <Icon>
            <KeyRound size={17} strokeWidth={1.6} aria-hidden="true" />
          </Icon>
          <span className="flex min-w-0 flex-1 flex-col leading-snug">
            <b className="font-semibold text-ink">Anthropic API key</b>
            <span className="text-secondary text-muted">Pay per token instead, from Credentials</span>
          </span>
          <Button asChild size="sm" variant="secondary">
            <Link to="/o/$org/connections/new" params={{ org }}>
              Add a key
            </Link>
          </Button>
        </li>
        <li className="flex items-center gap-3 border-t border-line py-3">
          <Icon>
            <Route size={17} strokeWidth={1.6} aria-hidden="true" />
          </Icon>
          <span className="flex min-w-0 flex-1 flex-col leading-snug">
            <b className="font-semibold text-ink">Another provider through a router</b>
            <span className="text-secondary text-muted">Bedrock, Vertex or an OpenAI-compatible endpoint</span>
          </span>
          <NotAvailable label="Set up">
            <Button size="sm" variant="secondary">
              Set up
            </Button>
          </NotAvailable>
        </li>
      </ul>
      <p className="m-0 mt-1 border-t border-line pt-3 pb-1 text-secondary text-ink-soft">
        Goal does not bill for usage. Model costs are billed by your provider; box time is billed by the cloud account
        Goal runs in. Amounts here are Goal's estimates from token counts.
      </p>
    </section>
  );
}

function Icon({ children }: { children: ReactNode }) {
  return (
    <span className="flex size-8 flex-none items-center justify-center rounded-cell border border-line bg-panel text-ink-soft">
      {children}
    </span>
  );
}
