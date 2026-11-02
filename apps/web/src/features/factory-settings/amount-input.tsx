// The dollar field of Budgets and targets: the text as typed while it has focus, the draft's value otherwise.
import { useState } from 'react';
import { Input } from '../../ui/field';

/** "60" from 60, "" from nothing: the field shows what the draft holds. */
const shown = (v: number | undefined) => (v === undefined ? '' : String(v));

/** The typed amount; nothing for an empty field, the old value while it is not a number. */
function amount(text: string): number | undefined | null {
  if (text.trim() === '') return undefined;
  const n = Number(text.replace(/^\$/, ''));
  return Number.isFinite(n) && n >= 0 ? n : null;
}

/**
 * A dollar amount: the text as typed while the field has focus ("3." on the way to "3.5"), the
 * draft's value otherwise; every number typed is an edit, an empty field removes the cap.
 */
export function AmountInput({
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
