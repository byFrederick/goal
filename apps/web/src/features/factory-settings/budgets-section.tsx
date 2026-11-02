// Settings › Budgets and targets: the spend caps and turn limit (review.budget) and the flow targets
// (line.flow.targets). Each field saves its own key and keeps the others of its object. The budget's
// "When a budget runs out" choice lives in the Flow group.
import type { Budget, Flow } from '../../shared/definition/model';
import { Field, Input } from '../../ui/field';
import { setBudget, setFlow } from '../factory-editor/draft/ops';
import { useDraftSession, useSection } from '../factory-editor/draft/use-draft';
import { prune } from '../factory-editor/line/flow-panel';
import { AmountInput } from '../factory-editor/review/budget-card';
import { SettingsGroup } from './settings-group';

export function BudgetsSection() {
  const session = useDraftSession();
  const review = useSection('review');
  const line = useSection('line');
  const budget: Budget = review.budget ?? {};
  const flow: Flow = line.flow ?? {};
  const flush = () => void session.flush();

  const setCap = <K extends keyof Budget>(field: K, value: Budget[K] | undefined) =>
    session.edit(setBudget(review, field, value), { coalesce: `/review/budget/${field}` });
  const setTarget = (field: keyof NonNullable<Flow['targets']>, value: number | undefined) =>
    session.edit(setFlow(line, prune({ ...flow, targets: { ...flow.targets, [field]: value } })), {
      coalesce: `/line/flow/targets/${field}`,
    });

  return (
    <SettingsGroup id="budgets" title="Budgets and targets">
      <div className="grid grid-cols-1 gap-x-5 gap-y-4 @[640px]:grid-cols-2">
        <h3 className="col-span-full m-0 text-sm font-medium">Budgets</h3>
        <Field label="Each day" help="Spend cap for one day, in US dollars." className="min-w-0">
          <AmountInput value={budget.daily_usd} onValue={(n) => setCap('daily_usd', n)} onDone={flush} />
        </Field>
        <Field label="Each week" help="Spend cap for seven days, in US dollars." className="min-w-0">
          <AmountInput value={budget.weekly_usd} onValue={(n) => setCap('weekly_usd', n)} onDone={flush} />
        </Field>
        <Field label="Each item" help="Spend cap for one run, in US dollars." className="min-w-0">
          <AmountInput value={budget.per_item_usd} onValue={(n) => setCap('per_item_usd', n)} onDone={flush} />
        </Field>
        <Field label="Turns per item" help="Most turns one run may take." className="min-w-0">
          <Input
            type="number"
            inputMode="numeric"
            min={1}
            step={1}
            value={budget.max_turns_per_task ?? ''}
            placeholder="No limit"
            onChange={(e) =>
              setCap('max_turns_per_task', e.target.value === '' ? undefined : Math.round(e.target.valueAsNumber))
            }
            onBlur={flush}
          />
        </Field>
      </div>
      <div className="grid grid-cols-1 gap-x-5 gap-y-4 @[640px]:grid-cols-2">
        <h3 className="col-span-full m-0 text-sm font-medium">Targets</h3>
        <Field label="Items a day" help="Items done per day. The Floor draws it as a target line." className="min-w-0">
          <Input
            type="number"
            inputMode="decimal"
            min={0}
            step="any"
            value={flow.targets?.throughput_per_day ?? ''}
            placeholder="No target"
            onChange={(e) =>
              setTarget('throughput_per_day', e.target.value === '' ? undefined : e.target.valueAsNumber)
            }
          />
        </Field>
        <Field
          label="Hours lead time"
          help="Hours from arriving to done. The Floor compares each week with it."
          className="min-w-0"
        >
          <Input
            type="number"
            inputMode="decimal"
            min={0}
            step="any"
            value={flow.targets?.lead_time_hours ?? ''}
            placeholder="No target"
            onChange={(e) => setTarget('lead_time_hours', e.target.value === '' ? undefined : e.target.valueAsNumber)}
          />
        </Field>
        <Field
          label="Expedite lead time, minutes"
          help="Minutes from arriving to done for expedite items."
          className="min-w-0"
        >
          <Input
            type="number"
            inputMode="decimal"
            min={0}
            step="any"
            value={flow.targets?.expedite_lead_time_minutes ?? ''}
            placeholder="No target"
            onChange={(e) =>
              setTarget('expedite_lead_time_minutes', e.target.value === '' ? undefined : e.target.valueAsNumber)
            }
          />
        </Field>
      </div>
    </SettingsGroup>
  );
}
