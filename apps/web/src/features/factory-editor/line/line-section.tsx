// Line (D §10.6, canvas Define · Line and New-Line): the stations in order, each with its kind,
// WIP limit, who works there and its rules; Add station; reordering by the ↑ ↓ buttons,
// Alt+ArrowUp/Down or dragging the row number; then the line preview, Flow and targets, and the
// checks on this line. The same view serves Definition and the wizard (D13); `?station=` selects
// and focuses a station (E's "Add an investigator").
import { useEffect, useId, useRef } from 'react';
import { useSection } from '../draft/use-draft';
import { SectionShell } from '../frame/section-shell';
import type { SectionViewProps } from '../section-views';
import { AddStationMenu } from './add-station-menu';
import { FlowPanel } from './flow-panel';
import { stationLabel } from './labels';
import { LineChecks } from './line-checks';
import { LineEditorProvider, useLineEditor } from './line-editor';
import { LinePreview } from './line-preview';
import { PresetControl } from './model-preset-control';
import { ModelsBox } from './models-box';
import { StationDnd } from './station-dnd';
import { lineColumns, StationRow } from './station-row';

export function LineSection({ place, search }: SectionViewProps) {
  return (
    <LineEditorProvider place={place}>
      <Stations selected={search.station} />
      {place === 'wizard' ? <ModelsBox preset={<PresetControl />} /> : null}
      <div className="flex flex-col gap-4">
        <LinePreview />
        <FlowPanel />
        <LineChecks />
      </div>
    </LineEditorProvider>
  );
}

function Stations({ selected }: { selected: string | undefined }) {
  const editor = useLineEditor();
  const basics = useSection('basics');
  const help = useId();
  const { stations, constraintNow, place } = editor;

  const constraint = stations.find((s) => s.id === constraintNow);
  const constraintSentence =
    place === 'definition' && constraint
      ? ` ${basics.name ?? 'This factory'}’s constraint right now is ${stationLabel(constraint)}.`
      : place === 'wizard'
        ? ' Agents pull work when a station is under its WIP limit; nobody assigns it.'
        : '';

  // ?station= scrolls to its row and puts focus on its name, once.
  const focused = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!selected || focused.current === selected) return;
    const row = document.querySelector(`[data-station="${CSS.escape(selected)}"]`);
    if (!row) return;
    focused.current = selected;
    row.scrollIntoView({ block: 'center' });
    row.querySelector<HTMLElement>('[data-station-name]')?.focus({ preventScroll: true });
  });

  return (
    <SectionShell
      section="line"
      descriptionId={help}
      description={
        <>
          Stations in the order work moves through them.{constraintSentence}
          {editor.viewOnly
            ? null
            : ' Move a station with its arrow buttons, or focus it and press Alt with the up or down arrow. Dragging its number works too.'}
        </>
      }
      aside={editor.viewOnly ? null : <AddStationMenu />}
    >
      <div className="@container -mx-1 flex min-w-0 flex-col gap-2 md:mx-0">
        <div
          aria-hidden="true"
          className={`hidden px-4 text-[13px] font-medium text-muted @[960px]:grid ${lineColumns}`}
        >
          <span />
          <span>Station</span>
          <span>Kind</span>
          <span>WIP limit</span>
          <span>Who works here</span>
          <span className="text-right">{editor.viewOnly ? null : 'Order'}</span>
        </div>
        {stations.length === 0 ? (
          <p className="m-0 rounded-control border border-dashed border-rule px-4 py-3.5 text-sm text-muted">
            No stations yet. Add a queue where work arrives, the stations that work on it, then Done.
          </p>
        ) : (
          <StationDnd>
            <ol
              aria-label="Stations, in order"
              aria-describedby={help}
              className="m-0 flex list-none flex-col gap-2 p-0"
            >
              {stations.map((s, i) => (
                <StationRow key={editor.keyOf(s.id)} station={s} index={i} selected={s.id === selected} />
              ))}
            </ol>
          </StationDnd>
        )}
        <p role="status" className="sr-only">
          {editor.announcement}
        </p>
      </div>
    </SectionShell>
  );
}
