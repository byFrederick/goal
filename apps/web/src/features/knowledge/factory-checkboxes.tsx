// "Factories that read it" on Add a source (ADR 0038 decision 12): one checkbox per factory. A
// checked factory gets the source in its draft when the page saves, never published; the right of
// each row says which. Rows keep a 44 px target on phones.
import type { Factory } from '../../api/types';
import { FactoryBadge } from '../../app/factory-badge';
import { factoryTitle } from '../../shared/factory';
import { factoryMark } from '../../shared/marks';
import { Checkbox } from '../../ui/checkbox';

export function FactoryCheckboxes({
  factories,
  checked,
  onChange,
  disabled = false,
}: {
  factories: readonly Pick<Factory, 'id' | 'name'>[];
  /** The ids of the factories that read it. */
  checked: readonly string[];
  onChange: (ids: string[]) => void;
  disabled?: boolean;
}) {
  const toggle = (id: string, on: boolean) => onChange(on ? [...checked, id] : checked.filter((c) => c !== id));

  return (
    <fieldset className="m-0 min-w-0 border-0 p-0">
      <legend className="sr-only">Factories that read it</legend>
      <ul className="m-0 flex list-none flex-col p-0">
        {factories.map((f) => {
          const on = checked.includes(f.id);
          return (
            <li
              key={f.id}
              className="flex min-h-11 items-center justify-between gap-3 border-b border-line px-5 py-2 last:border-b-0"
            >
              <Checkbox
                checked={on}
                disabled={disabled}
                onCheckedChange={(v) => toggle(f.id, v)}
                label={
                  <span className="flex min-w-0 items-center gap-2">
                    <FactoryBadge mark={factoryMark(f, factories)} />
                    <span className="break-words">{factoryTitle(f)}</span>
                  </span>
                }
                className="min-h-11 min-w-0"
              />
              <span className="shrink-0 text-secondary text-muted">{on ? 'Into its draft' : 'Not listed'}</span>
            </li>
          );
        })}
      </ul>
    </fieldset>
  );
}
