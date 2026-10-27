// Budget (D §10.10, boards Define-Publish and New-Review): the review section's weekly and
// per-item caps and what happens when the week's budget runs out (drain or stop), saved with the
// draft; in Definition the week's spend against the running cap (D1's enforcement, the Board's
// LineBudget), and Claude slots: the agents this line runs at full WIP against the factory's or
// the organization's cap, with what the other factories need at the same time.
import { useQueries, useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { useId, useState } from 'react';
import { boardQuery, factoriesQuery, settingsQuery, versionModelQuery } from '../../../api/queries';
import type { Budget, LineSection } from '../../../shared/definition/model';
import { lineSlots, slotCap } from '../../../shared/definition/slots';
import { formatUsd, formatUsdCompact, plural } from '../../../shared/format';
import { useOrg } from '../../../shared/org';
import { SectionCard } from '../../../ui/card';
import { Field, Input } from '../../../ui/field';
import { Meter } from '../../../ui/hatch';
import { setBudget } from '../draft/ops';
import { useDraftSession, useModel, useSection } from '../draft/use-draft';
import { useEditorFacts } from './use-review';

/** "60" from 60, "" from nothing: the field shows what the draft holds. */
const shown = (v: number | undefined) => (v === undefined ? '' : String(v));

/** The typed amount; nothing for an empty field, the old value while it is not a number. */
function amount(text: string): number | undefined | null {
  if (text.trim() === '') return undefined;
  const n = Number(text.replace(/^\$/, ''));
  return Number.isFinite(n) && n >= 0 ? n : null;
}

export function BudgetCard({ name }: { name: string }) {
  const session = useDraftSession();
  const review = useSection('review');
  const budget: Budget = review.budget ?? {};
  const id = useId();
  const set = <K extends keyof Budget>(field: K, value: Budget[K] | undefined) =>
    session.edit(setBudget(review, field, value), { coalesce: `/review/budget/${field}` });

  const flush = () => void session.flush();

  return (
    <SectionCard title="Budget">
      <div className="grid gap-4 @[420px]:grid-cols-2">
        <Field label="Each week" help="Spend of the last 7 days, in US dollars">
          <AmountInput value={budget.weekly_usd} onValue={(n) => set('weekly_usd', n)} onDone={flush} />
        </Field>
        <Field label="Each item" help="Spend cap for one run">
          <AmountInput value={budget.per_item_usd} onValue={(n) => set('per_item_usd', n)} onDone={flush} />
        </Field>
      </div>
      <fieldset className="m-0 flex flex-col gap-1 border-0 p-0">
        <legend className="mb-2 text-sm font-medium">When the week’s budget runs out</legend>
        {(
          [
            ['drain', 'Finish items in progress, take no new ones'],
            ['stop', 'Stop every agent at once'],
          ] as const
        ).map(([value, label]) => (
          <label key={value} className="flex min-h-11 cursor-pointer items-center gap-3 text-body md:min-h-9">
            <input
              type="radio"
              name={`${id}-exhausted`}
              value={value}
              checked={(budget.on_exhausted ?? 'drain') === value}
              onChange={() => set('on_exhausted', value)}
              className="size-4 accent-ink"
            />
            {label}
          </label>
        ))}
      </fieldset>
      <WeekSpend />
      <ClaudeSlots name={name} />
    </SectionCard>
  );
}

/** The running version's week: spend against its cap, and whether agent work has stopped. */
function WeekSpend() {
  const { org, factory, current } = useEditorFacts();
  const board = useQuery({ ...boardQuery(org, factory), enabled: current !== null }).data;
  const b = board?.line.budget;
  if (!b) return null;
  const spent = `${formatUsd(b.spent_usd)} of ${formatUsdCompact(b.weekly_usd)}`;
  const label = `${spent} spent in the last 7 days`;
  return (
    <div className="flex flex-col gap-2 border-t border-line pt-3">
      <div className="flex justify-between gap-3 text-sm">
        <span>Spent in the last 7 days</span>
        <span className="tabular-nums">{spent}</span>
      </div>
      <Meter value={b.spent_usd} max={b.weekly_usd} tone={b.exhausted ? 'rework' : 'run'} label={label} />
      {b.exhausted ? (
        <p className="m-0 text-secondary text-rework-ink">
          Used up: no new agent work starts until the week’s spend falls under the cap or you raise it.
        </p>
      ) : null}
    </div>
  );
}

/**
 * A dollar amount: the text as typed while the field has focus ("3." on the way to "3.5"), the
 * draft's value otherwise; every number typed is an edit, an empty field removes the cap.
 */
function AmountInput({
  value,
  onValue,
  onDone,
}: {
  value: number | undefined;
  onValue: (n: number | undefined) => void;
  onDone: () => void;
}) {
  const [typed, setTyped] = useState<string | null>(null);
  return (
    <div className="relative">
      <span aria-hidden="true" className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted">
        $
      </span>
      <Input
        inputMode="decimal"
        className="pl-6 tabular-nums"
        value={typed ?? shown(value)}
        placeholder="No cap"
        onFocus={() => setTyped(shown(value))}
        onChange={(e) => {
          setTyped(e.target.value);
          const n = amount(e.target.value);
          if (n !== null) onValue(n);
        }}
        onBlur={() => {
          setTyped(null);
          onDone();
        }}
      />
    </div>
  );
}

const list = new Intl.ListFormat('en', { type: 'conjunction' });

/** Claude slots at full WIP against the cap, with the organization's other factories at peak. */
function ClaudeSlots({ name }: { name: string }) {
  const { org, factory } = useEditorFacts();
  const orgCtx = useOrg();
  const model = useModel();
  const slots = useQuery(settingsQuery(org)).data?.slots;
  const others = (useQuery(factoriesQuery(org)).data ?? []).filter((f) => f.id !== factory && f.current);
  const otherLines = useQueries({
    queries: others.map((f) => ({ ...versionModelQuery(org, f.id, 'current'), enabled: slots?.max != null })),
  });
  const cap = slotCap(slots, factory);
  if (!cap) return null;
  const used = lineSlots(model.line);
  const mine = Math.min(used, cap.cap);
  const peers = others.map((f, i) => ({
    name: f.name,
    slots: lineSlots((otherLines[i]?.data?.model.line ?? {}) as unknown as LineSection),
  }));
  const total = used + peers.reduce((n, p) => n + p.slots, 0);
  const busy = peers.filter((p) => p.slots > 0);
  const orgMax = slots?.max ?? null;
  const together = list.format([name, ...busy.map((p) => p.name)]);
  const sharing =
    orgMax === null || busy.length === 0
      ? cap.scope === 'factory'
        ? `${name} runs at most ${plural(cap.cap, 'agent')} at once; its own cap.`
        : null
      : total <= orgMax
        ? `At peak ${together} need ${total} of the organization’s ${orgMax} Claude slots.`
        : `At peak ${together} need ${total} Claude slots, more than the organization’s ${orgMax}: items wait for a free one.`;
  const label = `Claude slots ${name} can use: ${mine} of ${cap.cap}`;
  return (
    <div className="flex flex-col gap-2 border-t border-line pt-3">
      <div className="flex justify-between gap-3 text-sm">
        <span>Claude slots {name} can use</span>
        <span className="tabular-nums">
          {mine} of {cap.cap}
        </span>
      </div>
      <Meter value={mine} max={cap.cap} label={label} />
      {sharing ? <p className="m-0 text-secondary text-muted">{sharing}</p> : null}
      <p className="m-0 text-secondary">
        <Link
          to="/o/$org/settings"
          params={{ org: orgCtx.slug }}
          className="underline underline-offset-3 transition-colors duration-200 hover:text-run"
        >
          Change in Organization settings
        </Link>
      </p>
    </div>
  );
}
