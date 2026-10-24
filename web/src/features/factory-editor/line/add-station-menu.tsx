// Add station (D §10.6): an agent station, a human station or a queue, inserted after the row that
// last had focus, else before Done; its name takes focus so it can be named at once.
import { Bot, ChevronDown, Inbox, Plus, Users } from 'lucide-react';
import type { StationKind } from '../../../shared/definition/model';
import { Button } from '../../../ui/button';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../../../ui/menu';
import { addStation } from '../draft/ops';
import { useLineEditor } from './line-editor';

const choices: { kind: Exclude<StationKind, 'done'>; label: string; name: string; Icon: typeof Bot }[] = [
  { kind: 'agent', label: 'Agent station', name: 'New station', Icon: Bot },
  { kind: 'human', label: 'Human station', name: 'Approve', Icon: Users },
  { kind: 'queue', label: 'Queue', name: 'Queue', Icon: Inbox },
];

export function AddStationMenu() {
  const editor = useLineEditor();

  const add = (kind: Exclude<StationKind, 'done'>, name: string) => {
    const after = editor.lastFocused.current;
    const known = after !== undefined && editor.stations.some((s) => s.id === after && s.kind !== 'done');
    const { edit, station } = addStation(editor.line, kind, { name, ...(known ? { after } : {}) });
    editor.insert(edit, station);
  };

  return (
    <Menu>
      <MenuTrigger asChild>
        <Button variant="primary" className="h-11 md:h-10">
          <Plus size={16} strokeWidth={1.5} aria-hidden="true" />
          Add station
          <ChevronDown size={14} strokeWidth={1.75} aria-hidden="true" />
        </Button>
      </MenuTrigger>
      <MenuContent side="bottom" align="end" className="w-60">
        {choices.map(({ kind, label, name, Icon }) => (
          <MenuItem key={kind} onSelect={() => add(kind, name)}>
            <Icon size={18} strokeWidth={1.5} aria-hidden="true" />
            {label}
          </MenuItem>
        ))}
      </MenuContent>
    </Menu>
  );
}
