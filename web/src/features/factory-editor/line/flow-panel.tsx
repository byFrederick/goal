// Flow and targets (D §10.6, E's request): how work is released into the line, which station paces
// it, the expedite lane, the andon threshold and E's flow targets (throughput, lead times). A
// closed disclosure under the preview. Each field says what the engine does with it; what 2.2
// stores but does not act on yet says so.
import { ChevronRight } from 'lucide-react';
import { useId } from 'react';
import type { Flow } from '../../../shared/definition/model';
import { Field, Input, NativeSelect } from '../../../ui/field';
import { SegmentedControl } from '../../../ui/segmented-control';
import { setFlow } from '../draft/ops';
import { EditedMark } from '../frame/section-shell';
import { useProblemText } from '../frame/use-editor';
import { stationLabel } from './labels';
import { useLineEditor } from './line-editor';

type Release = 'push' | 'drum-buffer-rope';

/** Drops empty objects so an emptied group leaves no `{}` behind in factory.yaml. */
function prune(flow: Flow): Flow {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(flow)) {
    if (v === undefined) continue;
    if (typeof v === 'object' && v !== null && !Array.isArray(v)) {
      const inner = Object.fromEntries(Object.entries(v).filter(([, x]) => x !== undefined));
      if (Object.keys(inner).length > 0) out[k] = inner;
    } else out[k] = v;
  }
  return out as Flow;
}

const number = (text: string): number | undefined => {
  const n = Number(text.replace(',', '.'));
  return text.trim() === '' || !Number.isFinite(n) || n < 0 ? undefined : n;
};

function NumberField({
  label,
  help,
  pointer,
  value,
  suffix,
  onChange,
}: {
  label: string;
  help: string;
  pointer: string;
  value: number | undefined;
  suffix?: string;
  onChange: (value: number | undefined) => void;
}) {
  const editor = useLineEditor();
  const error = useProblemText(pointer);
  return (
    <Field
      label={label}
      help={help}
      error={error}
      aside={<EditedMark section="line" pointer={pointer} label={label.toLowerCase()} />}
    >
      <span className="flex items-center gap-2">
        <Input
          inputMode="decimal"
          value={value ?? ''}
          onChange={(e) => onChange(number(e.target.value))}
          onBlur={editor.flush}
          autoComplete="off"
          className="w-28 tabular-nums"
        />
        {suffix ? <span className="text-sm text-muted">{suffix}</span> : null}
      </span>
    </Field>
  );
}

export function FlowPanel() {
  const editor = useLineEditor();
  const id = useId();
  const flow = editor.line.flow ?? {};
  const release: Release = flow.release === 'drum-buffer-rope' ? 'drum-buffer-rope' : 'push';
  const paced = editor.stations.filter((s) => s.kind === 'agent' || s.kind === 'human');
  const expediteTypes = flow.expedite?.types ?? [];

  const write = (next: Record<string, unknown>, coalesce?: string) =>
    editor.edit(
      setFlow(editor.line, prune(next as Flow)),
      coalesce ? { coalesce: `/line/flow/${coalesce}` } : undefined,
    );

  return (
    <details className="group @container min-w-0 rounded-card border border-line bg-card">
      <summary className="flex min-h-14 cursor-pointer list-none items-center gap-2 rounded-card px-4 py-3 md:px-5 [&::-webkit-details-marker]:hidden">
        <ChevronRight
          size={16}
          strokeWidth={1.75}
          aria-hidden="true"
          className="shrink-0 transition-transform duration-200 group-open:rotate-90"
        />
        <span className="flex min-w-0 flex-col">
          <span className="text-lg font-medium">Flow and targets</span>
          <span className="text-[13px] text-muted">
            How work enters the line, what paces it, the expedite lane, when to stop the line and what to aim for.
          </span>
        </span>
      </summary>
      <div className="grid grid-cols-1 gap-x-5 gap-y-5 border-t border-line px-4 pt-4 pb-5 md:px-5 @[640px]:grid-cols-2">
        <div className="flex min-w-0 flex-col gap-1.5">
          <div className="flex min-h-6 flex-wrap items-center justify-between gap-x-3">
            <span id={`${id}-release`} className="text-sm font-medium">
              Release
            </span>
            <EditedMark section="line" pointer="/line/flow/release" label="the release policy" />
          </div>
          <SegmentedControl
            label="Release"
            value={release}
            onChange={(v) => write({ ...flow, release: v === 'push' ? undefined : v })}
            options={[
              { value: 'push', label: 'Push' },
              { value: 'drum-buffer-rope', label: 'Drum-buffer-rope' },
            ]}
            className="flex max-w-sm [&>label]:flex-1 [&>label]:justify-center"
          />
          <p className="m-0 text-[13px] leading-snug text-muted">
            Push lets work in as it arrives. Drum-buffer-rope lets it in only as fast as the constraint takes it,
            keeping the buffer in front of it full. Saved now, not enforced yet: work is released as it arrives.
          </p>
        </div>

        <Field
          label="Constraint"
          help="The station that paces the line. The Board and the Floor measure flow against it. Auto picks the station with the most work per slot."
          error={useProblemText('/line/flow/constraint')}
          aside={<EditedMark section="line" pointer="/line/flow/constraint" label="the constraint" />}
        >
          <NativeSelect
            value={flow.constraint ?? ''}
            onChange={(e) => write({ ...flow, constraint: e.target.value || undefined })}
          >
            <option value="">Auto</option>
            {paced.map((s) => (
              <option key={s.id} value={s.id}>
                {stationLabel(s)}
              </option>
            ))}
            {flow.constraint && !paced.some((s) => s.id === flow.constraint) ? (
              <option value={flow.constraint}>{flow.constraint} (not a station)</option>
            ) : null}
          </NativeSelect>
        </Field>

        <fieldset className="m-0 flex min-w-0 flex-col gap-1.5 border-0 p-0">
          <legend className="mb-1.5 flex w-full flex-wrap items-center justify-between gap-x-3 p-0 text-sm font-medium">
            Expedite work types
            <EditedMark section="line" pointer="/line/flow/expedite/types" label="the expedite work types" />
          </legend>
          {editor.workTypes.length > 0 ? (
            <div className="flex flex-wrap gap-x-4 gap-y-1">
              {editor.workTypes.map((t) => (
                <label key={t} className="inline-flex min-h-11 cursor-pointer items-center gap-2 text-sm md:min-h-8">
                  <input
                    type="checkbox"
                    checked={expediteTypes.includes(t)}
                    onChange={(e) => {
                      const types = e.target.checked ? [...expediteTypes, t] : expediteTypes.filter((x) => x !== t);
                      write({ ...flow, expedite: { ...flow.expedite, types: types.length > 0 ? types : undefined } });
                    }}
                    className="size-4 accent-ink"
                  />
                  {t}
                </label>
              ))}
            </div>
          ) : (
            <p className="m-0 text-sm text-muted">Add work types in Basics to choose which ones jump the queue.</p>
          )}
          <p className="m-0 text-[13px] leading-snug text-muted">
            Items of these types are pulled before any other work at every station.
          </p>
        </fieldset>

        <NumberField
          label="Expedite limit"
          help="Expedite items in the line at once. Saved now, not enforced yet; the Board shows it."
          pointer="/line/flow/expedite/wip"
          value={flow.expedite?.wip}
          suffix="items"
          onChange={(wip) =>
            write(
              { ...flow, expedite: { ...flow.expedite, wip: wip === undefined ? undefined : Math.round(wip) } },
              'expedite/wip',
            )
          }
        />

        <NumberField
          label="Andon at rework rate"
          help="Stops the line when this share of recent items was sent back. Saved now, not enforced yet: the Board shows it as a warning."
          pointer="/line/flow/andon/rework_rate"
          value={flow.andon?.rework_rate === undefined ? undefined : Math.round(flow.andon.rework_rate * 100)}
          suffix="%"
          onChange={(pct) =>
            write(
              {
                ...flow,
                andon: { ...flow.andon, rework_rate: pct === undefined ? undefined : Math.min(pct, 100) / 100 },
              },
              'andon/rework_rate',
            )
          }
        />
        <NumberField
          label="Andon window"
          help="How many of the latest finished items the rework rate counts."
          pointer="/line/flow/andon/window"
          value={flow.andon?.window}
          suffix="items"
          onChange={(window) =>
            write(
              { ...flow, andon: { ...flow.andon, window: window === undefined ? undefined : Math.round(window) } },
              'andon/window',
            )
          }
        />

        <NumberField
          label="Throughput target"
          help="Items done per day. The Floor draws it as the target line on weekly throughput."
          pointer="/line/flow/targets/throughput_per_day"
          value={flow.targets?.throughput_per_day}
          suffix="per day"
          onChange={(v) =>
            write({ ...flow, targets: { ...flow.targets, throughput_per_day: v } }, 'targets/throughput')
          }
        />
        <NumberField
          label="Lead time target"
          help="Hours from arriving to done. The Floor compares each week's lead time with it."
          pointer="/line/flow/targets/lead_time_hours"
          value={flow.targets?.lead_time_hours}
          suffix="hours"
          onChange={(v) => write({ ...flow, targets: { ...flow.targets, lead_time_hours: v } }, 'targets/lead')}
        />
        <NumberField
          label="Expedite lead time target"
          help="Minutes from arriving to done for expedite items."
          pointer="/line/flow/targets/expedite_lead_time_minutes"
          value={flow.targets?.expedite_lead_time_minutes}
          suffix="minutes"
          onChange={(v) =>
            write({ ...flow, targets: { ...flow.targets, expedite_lead_time_minutes: v } }, 'targets/expedite')
          }
        />
      </div>
    </details>
  );
}
