// Who works at a station (D §10.6): an agent station's agent (or its lanes, or a new agent made on
// the spot), the factory's approvers at a human station (a factory role, spec 3 §10.2), the
// queue's intake (read-only beyond manual until connectors arrive), and the librarian at Done.
// View only, the choices are shown, not offered.
import { Link } from '@tanstack/react-router';
import { Users } from 'lucide-react';
import { useState } from 'react';
import type { ModelStation } from '../../../shared/definition/model';
import { cn } from '../../../ui/cn';
import { Select } from '../../../ui/select';
import { touchLinkWide } from '../../../ui/touch-target';
import { addAgent, setLibrarian, setRoute, setStationField, slug } from '../draft/ops';
import { useSection } from '../draft/use-draft';
import { hasExternalIntake, intakeSummary, stationLabel } from './labels';
import { useLineEditor } from './line-editor';
import { LanesDialog, TextDialog } from './station-dialogs';

const routed = '__route';
const create = '__new';

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
  const factory = useSection('basics').name ?? 'this factory';
  const { line, agents } = editor;
  const label = stationLabel(station);
  const [newAgent, setNewAgent] = useState(false);
  const [lanes, setLanes] = useState(false);

  if (station.kind === 'human') {
    return (
      <span className="flex min-h-9 items-center gap-2 text-sm text-ink-soft">
        <Users size={16} strokeWidth={1.5} aria-hidden="true" className="text-muted" />
        Approvers of {factory}
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
            className={cn('text-[13px] text-ink-soft underline underline-offset-3 hover:text-link', touchLinkWide)}
          >
            Edit in YAML
            <span className="sr-only">: the intake of {label}</span>
          </Link>
        ) : null}
      </span>
    );
  }

  if (station.kind === 'done') {
    return (
      <span className="flex min-w-0 flex-col gap-1">
        <Select
          aria-label={`Who works at ${label}`}
          value={line.librarian ?? ''}
          disabled={editor.viewOnly}
          onValueChange={(next) => editor.edit(setLibrarian(line, next || null))}
          options={[{ value: '', label: 'Nobody, items close' }, ...agents.map((a) => ({ value: a, label: a }))]}
        />
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
      <Select
        aria-label={`Who works at ${label}`}
        aria-invalid={invalid || undefined}
        aria-describedby={describedBy}
        value={value}
        disabled={editor.viewOnly}
        onValueChange={(v) => {
          if (v === create) setNewAgent(true);
          else if (v === routed) setLanes(true);
          else {
            const cleared = setRoute(line, station.id, null).value;
            editor.edit(setStationField(cleared, station.id, 'agent', v || undefined));
          }
        }}
        options={[
          ...(value === '' ? [{ value: '', label: 'Choose an agent' }] : []),
          ...agents.map((a) => ({ value: a, label: a === station.agent && count > 1 ? `${a} × ${count}` : a })),
          ...(station.agent && !taken.has(station.agent)
            ? [{ value: station.agent, label: `${station.agent} (not an agent)` }]
            : []),
          {
            value: routed,
            label:
              lanesCount > 0
                ? `${lanesCount} ${lanesCount === 1 ? 'lane' : 'lanes'}, routed by lane`
                : 'Routed by lane…',
          },
          { value: create, label: '+ New agent…' },
        ]}
      />
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
