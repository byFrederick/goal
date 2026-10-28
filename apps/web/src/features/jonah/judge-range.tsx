// A range input for the judge page's Pass mark and Sample. MASTER.md has no slider, so this one is local to
// the judge page: a native range input, styled with the accent and the tokens only, with its value shown
// beside it. It takes its id and its help line from the Field around it.
import { useFieldControl } from '../../ui/field';

export function RangeInput({
  label,
  min,
  max,
  step,
  value,
  shown,
  onChange,
}: {
  /** The accessible name, which the Field's label already gives; kept for the input's own aria-label. */
  label: string;
  min: number;
  max: number;
  step: number;
  value: number;
  /** The value as the person reads it: "0.7", "25%". */
  shown: string;
  onChange: (value: number) => void;
}) {
  const field = useFieldControl();
  return (
    <div className="flex items-center gap-3">
      <input
        id={field?.id}
        aria-describedby={field?.describedBy}
        aria-label={label}
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className="h-2 min-w-0 flex-1 cursor-pointer accent-run"
      />
      <span
        aria-hidden="true"
        className="min-w-14 rounded-chip border border-rule bg-card px-2 py-1 text-center font-numeral font-semibold tabular-nums"
      >
        {shown}
      </span>
    </div>
  );
}
