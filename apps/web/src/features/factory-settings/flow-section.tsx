// Settings › Flow: four choices of how work moves through the line. The budget's on_exhausted
// (review.budget) and release, assemblies.overlap (line.flow) and the expedite types (line.flow.expedite)
// each save their own key; a write keeps the other keys of its object, fresh_base included.
import { useId } from 'react';
import type { Flow } from '../../shared/definition/model';
import { SegmentedControl } from '../../ui/segmented-control';
import { TokenField } from '../factory-editor/basics/token-field';
import { setBudget, setFlow } from '../factory-editor/draft/ops';
import { useDraftSession, useSection, useViewOnly } from '../factory-editor/draft/use-draft';
import { prune } from '../factory-editor/line/flow-panel';
import { SettingsGroup } from './settings-group';

const exhausted = [
  { value: 'drain', label: 'Finish what started' },
  { value: 'stop', label: 'Stop at once' },
] as const;
const release = [
  { value: 'drum-buffer-rope', label: 'Pull when the constraint has room' },
  { value: 'push', label: 'As soon as it arrives' },
] as const;
const overlap = [
  { value: 'serialize', label: 'Take turns' },
  { value: 'allow', label: 'Work side by side' },
] as const;

/** One choice: its name, the segmented control and what the choice does. */
function Choice<T extends string>({
  name,
  options,
  value,
  onChange,
  help,
}: {
  name: string;
  options: readonly { value: T; label: string }[];
  value: T;
  onChange: (value: T) => void;
  help: string;
}) {
  const helpId = useId();
  const viewOnly = useViewOnly();
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <span aria-hidden="true" className="text-sm font-medium">
        {name}
      </span>
      <SegmentedControl
        label={name}
        options={options}
        value={value}
        readOnly={viewOnly}
        describedBy={helpId}
        onChange={onChange}
        className="max-w-full self-start"
      />
      <p id={helpId} className="m-0 text-[13px] leading-snug text-muted">
        {help}
      </p>
    </div>
  );
}

export function FlowSection() {
  const session = useDraftSession();
  const review = useSection('review');
  const line = useSection('line');
  const basics = useSection('basics');
  const flow: Flow = line.flow ?? {};
  const exhaustedValue = review.budget?.on_exhausted ?? 'drain';
  const releaseValue = flow.release === 'drum-buffer-rope' ? 'drum-buffer-rope' : 'push';
  const overlapValue = flow.assemblies?.overlap ?? 'serialize';
  const expediteTypes = flow.expedite?.types ?? [];
  const workTypes = basics.work_types ?? [];

  return (
    <SettingsGroup id="flow" title="Flow">
      <Choice
        name="When a budget runs out"
        options={exhausted}
        value={exhaustedValue}
        onChange={(v) =>
          session.edit(setBudget(review, 'on_exhausted', v), { coalesce: '/review/budget/on_exhausted' })
        }
        help="Finish what started lets running items reach the next station; nothing new starts. Stop at once stops every agent."
      />
      <Choice
        name="New work enters"
        options={release}
        value={releaseValue}
        onChange={(v) =>
          session.edit(setFlow(line, prune({ ...flow, release: v })), { coalesce: '/line/flow/release' })
        }
        help="Push lets work in as it arrives. Drum-buffer-rope lets it in only as fast as the constraint takes it. Saved now, not enforced yet: work is released as it arrives."
      />
      <Choice
        name="Items that touch the same files"
        options={overlap}
        value={overlapValue}
        onChange={(v) =>
          session.edit(setFlow(line, prune({ ...flow, assemblies: { ...flow.assemblies, overlap: v } })), {
            coalesce: '/line/flow/assemblies/overlap',
          })
        }
        help="Take turns: an item whose files overlap an item in flight waits for it. Work side by side: they build in parallel."
      />
      <TokenField
        label="Expedite lane"
        noun="type"
        values={expediteTypes}
        placeholder="Work type…"
        check={(t) => (workTypes.includes(t) ? undefined : 'Only a work type this factory has.')}
        help="Work of these types jumps the line."
        onAdd={(t) =>
          session.edit(setFlow(line, prune({ ...flow, expedite: { ...flow.expedite, types: [...expediteTypes, t] } })))
        }
        onRemove={(t) =>
          session.edit(
            setFlow(
              line,
              prune({
                ...flow,
                expedite: { ...flow.expedite, types: expediteTypes.filter((x) => x !== t) },
              }),
            ),
          )
        }
      />
    </SettingsGroup>
  );
}
