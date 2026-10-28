// The fields of step 5 (docs/design/screens/get-started/budget.jpg): the three budget sliders with their hints,
// the estimate, what happens when a budget runs out, and the two flow targets. Each control writes its field to
// the draft as it changes and flushes when the person leaves it; Continue (budget-step.tsx) publishes.
import { useState } from 'react';
import type { Budget } from '../../shared/definition/model';
import { formatUsd, formatUsdCompact, plural } from '../../shared/format';
import { Field, Input } from '../../ui/field';
import { RadioCards } from '../../ui/radio-cards';
import { useDraftSession, useSection } from '../factory-editor/draft/use-draft';
import { setBudgetField, setTarget } from './budget-draft';
import {
  averageItemUsd,
  budgetRanges,
  estimateForTarget,
  fullDaysHint,
  itemsHint,
  targetFallbacks,
  timesAverageHint,
} from './budget-hints';

/** The budget fields as the draft holds them, with the picture's value for each unset one. */
export function useBudgetValues() {
  const review = useSection('review');
  const line = useSection('line');
  const budget: Budget = review.budget ?? {};
  return {
    review,
    line,
    day: budget.daily_usd ?? budgetRanges.day.fallback,
    week: budget.weekly_usd ?? budgetRanges.week.fallback,
    perItem: budget.per_item_usd ?? budgetRanges.perItem.fallback,
    exhausted: budget.on_exhausted ?? 'drain',
    itemsPerDay: line.flow?.targets?.throughput_per_day ?? targetFallbacks.throughputPerDay,
    leadHours: line.flow?.targets?.lead_time_hours ?? targetFallbacks.leadTimeHours,
  };
}

export function BudgetFields() {
  const session = useDraftSession();
  const values = useBudgetValues();
  const { review, line, day, week, perItem, exhausted, itemsPerDay, leadHours } = values;

  // Each change is one edit, coalesced while the person drags or types; leaving the control saves it.
  const setBudgetAt = <K extends keyof Budget>(field: K, value: Budget[K]) =>
    session.edit(setBudgetField(review, field, value), { coalesce: `/review/budget/${field}` });
  const setTargetAt = (field: 'throughput_per_day' | 'lead_time_hours', value: number) =>
    session.edit(setTarget(line, field, value), { coalesce: `/line/flow/targets/${field}` });
  const flush = () => void session.flush();

  return (
    <>
      <BudgetSlider
        id="budget-day"
        label="A day"
        range={budgetRanges.day}
        value={day}
        hint={itemsHint(day)}
        onChange={(v) => setBudgetAt('daily_usd', v)}
        onDone={flush}
      />
      <BudgetSlider
        id="budget-week"
        label="A week"
        range={budgetRanges.week}
        value={week}
        hint={fullDaysHint(day, week)}
        onChange={(v) => setBudgetAt('weekly_usd', v)}
        onDone={flush}
      />
      <BudgetSlider
        id="budget-item"
        label="Most per item"
        range={budgetRanges.perItem}
        value={perItem}
        hint={timesAverageHint(perItem)}
        onChange={(v) => setBudgetAt('per_item_usd', v)}
        onDone={flush}
      />

      <Estimate itemsPerDay={itemsPerDay} />

      <RadioCards
        legend="When a budget runs out"
        name="budget-exhausted"
        options={[
          {
            value: 'drain',
            label: "Finish what's started",
            description: 'Running items finish; nothing new starts until tomorrow.',
          },
          { value: 'stop', label: 'Stop everything', description: 'Running agents pause at their next turn.' },
        ]}
        value={exhausted}
        onChange={(v) => {
          setBudgetAt('on_exhausted', v);
          flush();
        }}
      />

      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Target, items a day">
          <WholeInput value={itemsPerDay} onValue={(n) => setTargetAt('throughput_per_day', n)} onDone={flush} />
        </Field>
        <Field label="Target lead time">
          <div className="flex items-center gap-3">
            <WholeInput value={leadHours} onValue={(n) => setTargetAt('lead_time_hours', n)} onDone={flush} />
            <span className="text-body text-muted">hours</span>
          </div>
        </Field>
      </div>
    </>
  );
}

/** One budget slider: its label and value, the hint, and the range's ends under it. */
function BudgetSlider({
  id,
  label,
  range,
  value,
  hint,
  onChange,
  onDone,
}: {
  id: string;
  label: string;
  range: { min: number; max: number; step: number };
  value: number;
  hint: string;
  onChange: (value: number) => void;
  onDone: () => void;
}) {
  const hintId = `${id}-hint`;
  const shown = formatUsdCompact(value);
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
        <label htmlFor={id} className="text-body font-medium text-ink">
          {label}
        </label>
        <span className="flex items-baseline gap-3">
          <span id={hintId} className="text-secondary text-muted">
            {hint}
          </span>
          <output htmlFor={id} className="font-numeral text-heading">
            {shown}
          </output>
        </span>
      </div>
      <input
        id={id}
        type="range"
        min={range.min}
        max={range.max}
        step={range.step}
        value={value}
        aria-describedby={hintId}
        aria-valuetext={`${shown}, ${hint}`}
        onChange={(e) => onChange(Number(e.target.value))}
        onPointerUp={onDone}
        onKeyUp={onDone}
        onBlur={onDone}
        className="w-full cursor-pointer accent-run"
      />
      <div className="flex justify-between text-secondary text-muted">
        <span>{formatUsdCompact(range.min)}</span>
        <span>{formatUsdCompact(range.max)}</span>
      </div>
    </div>
  );
}

/** "~$31 a day reaches your target of 10 items": the target at the average cost. */
function Estimate({ itemsPerDay }: { itemsPerDay: number }) {
  const estimate = estimateForTarget(itemsPerDay);
  return (
    <div className="flex items-center gap-4 rounded-card bg-panel p-4">
      <span className="font-numeral text-kpi text-run-ink">~{formatUsdCompact(estimate)}</span>
      <span className="text-secondary text-ink-soft">
        a day reaches your target of {plural(itemsPerDay, 'item')}, at about {formatUsd(averageItemUsd)} each for a
        typical item.
      </span>
    </div>
  );
}

/**
 * A whole number typed into a field. The text is kept while the field has focus, so an empty or partial
 * entry does not snap back; a number that is a whole positive value is saved as it is typed.
 */
function WholeInput({ value, onValue, onDone }: { value: number; onValue: (n: number) => void; onDone: () => void }) {
  const [typed, setTyped] = useState<string | null>(null);
  return (
    <Input
      inputMode="numeric"
      autoComplete="off"
      value={typed ?? String(value)}
      onChange={(e) => {
        setTyped(e.target.value);
        const n = Number(e.target.value);
        if (Number.isInteger(n) && n >= 1) onValue(n);
      }}
      onBlur={() => {
        setTyped(null);
        onDone();
      }}
    />
  );
}
