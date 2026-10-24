// Who works at a station (D §10.6): an agent station's agent (or its lanes, or a new agent made on
// the spot), the person deciding at a human station (the local user until phase 3), the queue's
// intake (read-only beyond manual until connectors arrive), and the librarian at Done.
import { Link } from '@tanstack/react-router';
import { Users } from 'lucide-react';
import { useState } from 'react';
import type { ModelStation } from '../../../shared/definition/model';
import { cn } from '../../../ui/cn';
import { addAgent, setLibrarian, setRoute, setStationField, slug } from '../draft/ops';
import { useSection } from '../draft/use-draft';
import { hasExternalIntake, intakeSummary, stationLabel } from './labels';
import { useLineEditor } from './line-editor';
import { LanesDialog, TextDialog } from './station-dialogs';

const routed = '__route';
const create = '__new';

const select = cn(
  'h-11 w-full min-w-0 cursor-pointer truncate rounded-control border border-rule bg-surface pr-8 pl-2.5 text-base text-ink transition-colors duration-200 md:h-9 md:text-sm',
  'hover:border-faint aria-[invalid=true]:border-rework',
);

export function WhoWorks({
  station,
  invalid,
  describedBy,
}: {
  station: ModelStation;
  invalid?: boolean;
  describedBy?: string | undefined;
}) {
  const editor = useLineEditor();
  const agentsSection = useSection('agents');
  const { line, agents } = editor;
  const label = stationLabel(station);
  const [newAgent, setNewAgent] = useState(false);
  const [lanes, setLanes] = useState(false);

  if (station.kind === 'human') {
    return (
      <span className="flex min-h-9 items-center gap-2 text-sm text-ink-soft">
        <Users size={16} strokeWidth={1.5} aria-hidden="true" className="text-muted" />
        You decide here
      </span>
    );
  }

  if (station.kind === 'queue') {
    const external = hasExternalIntake(line.intake);
    return (
      <span className="flex min-h-9 min-w-0 flex-col justify-center text-sm leading-snug">
        <span className="line-clamp-2 min-w-0">{intakeSummary(line.intake)}</span>
        {external ? (
          <Link
            to="."
            search={(prev) => ({ ...prev, mode: 'yaml' as const })}
            className="text-[13px] text-ink-soft underline underline-offset-3 hover:text-run"
            aria-label={`Edit the intake of ${label} in YAML`}
          >
            Edit in YAML
          </Link>
        ) : null}
      </span>
    );
  }

  if (station.kind === 'done') {
    return (
      <span className="flex min-w-0 flex-col gap-1">
        <select
          aria-label={`Who works at ${label}`}
          value={line.librarian ?? ''}
          onChange={(e) => editor.edit(setLibrarian(line, e.target.value || null))}
          className={select}
        >
          <option value="">Nobody, items close</option>
          {agents.map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </select>
        {line.librarian ? (
          <span className="text-[13px] leading-snug text-muted">Runs when the librarian ships</span>
        ) : null}
      </span>
    );
  }

  const lanesCount = Object.keys(station.route ?? {}).length;
  const count = station.agents ?? 1;
  const value = lanesCount > 0 ? routed : (station.agent ?? '');
  const taken = new Set(agents);

  return (
    <>
      <select
        aria-label={`Who works at ${label}`}
        aria-invalid={invalid || undefined}
        aria-describedby={describedBy}
        value={value}
        onChange={(e) => {
          const v = e.target.value;
          if (v === create) setNewAgent(true);
          else if (v === routed) setLanes(true);
          else {
            const cleared = setRoute(line, station.id, null).value;
            editor.edit(setStationField(cleared, station.id, 'agent', v || undefined));
          }
        }}
        className={select}
      >
        {value === '' ? <option value="">Choose an agent</option> : null}
        {agents.map((a) => (
          <option key={a} value={a}>
            {a === station.agent && count > 1 ? `${a} × ${count}` : a}
          </option>
        ))}
        {station.agent && !taken.has(station.agent) ? (
          <option value={station.agent}>{station.agent} (not an agent)</option>
        ) : null}
        <option value={routed}>
          {lanesCount > 0 ? `${lanesCount} ${lanesCount === 1 ? 'lane' : 'lanes'}, routed by lane` : 'Routed by lane…'}
        </option>
        <option value={create}>+ New agent…</option>
      </select>
      <TextDialog
        open={newAgent}
        onOpenChange={setNewAgent}
        title={`New agent for ${label}`}
        description="It gets a prompt file you write in Agents."
        label="Agent name"
        help="Lowercase letters, digits and dashes, as in cost-analyst."
        submit="Add agent"
        mono
        check={(v) => {
          if (!v) return 'Name the agent.';
          if (slug(v) !== v) return 'Use lowercase letters, digits and dashes, as in cost-analyst.';
          if (taken.has(v)) return `${v} is already an agent. Choose it from the list instead.`;
          return undefined;
        }}
        onSave={(name) => {
          const cleared = setRoute(line, station.id, null).value;
          editor.edit([addAgent(agentsSection, name), setStationField(cleared, station.id, 'agent', name)]);
        }}
      />
      <LanesDialog
        open={lanes}
        onOpenChange={setLanes}
        station={label}
        route={station.route}
        agents={agents}
        suggestions={editor.workTypes}
        onSave={(route) => {
          const next = setRoute(line, station.id, route);
          editor.edit(route ? setStationField(next.value, station.id, 'agent', undefined) : next);
        }}
      />
    </>
  );
}
