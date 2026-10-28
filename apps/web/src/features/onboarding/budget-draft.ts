// The draft edits step 5 makes: the budget fields of the review section and the flow targets of the line
// section, with the picture's defaults for the fields the draft leaves unset (Continue writes them before it
// publishes, so what the person saw is what goes live).
import type { Budget, LineSection, ReviewSection } from '../../shared/definition/model';
import { type AnyEdit, setBudget, setFlow } from '../factory-editor/draft/ops';
import { budgetRanges, targetFallbacks } from './budget-hints';

export function setBudgetField<K extends keyof Budget>(review: ReviewSection, field: K, value: Budget[K]) {
  return setBudget(review, field, value);
}

/** One flow target (items a day, lead time in hours). The flow keeps its other fields. */
export function setTarget(line: LineSection, field: 'throughput_per_day' | 'lead_time_hours', value: number) {
  const flow = line.flow ?? {};
  return setFlow(line, { ...flow, targets: { ...flow.targets, [field]: value } });
}

/**
 * The edits that fill in the defaults for the budget and target fields the draft leaves unset. Each budget
 * edit starts from the previous one's value, so the edits compose.
 */
export function defaultEdits(review: ReviewSection, line: LineSection): AnyEdit[] {
  const edits: AnyEdit[] = [];
  let next = review;
  const fill = <K extends keyof Budget>(field: K, value: NonNullable<Budget[K]>) => {
    if (next.budget?.[field] !== undefined) return;
    const edit = setBudget(next, field, value);
    edits.push(edit);
    next = edit.value;
  };
  fill('daily_usd', budgetRanges.day.fallback);
  fill('weekly_usd', budgetRanges.week.fallback);
  fill('per_item_usd', budgetRanges.perItem.fallback);
  fill('on_exhausted', 'drain');

  // One flow edit for both targets: two edits from the same line would overwrite each other.
  const flow = line.flow ?? {};
  const targets = flow.targets ?? {};
  if (targets.throughput_per_day === undefined || targets.lead_time_hours === undefined) {
    edits.push(
      setFlow(line, {
        ...flow,
        targets: {
          ...targets,
          throughput_per_day: targets.throughput_per_day ?? targetFallbacks.throughputPerDay,
          lead_time_hours: targets.lead_time_hours ?? targetFallbacks.leadTimeHours,
        },
      }),
    );
  }
  return edits;
}
