// The YAML view's file tabs (D §10.9): factory.yaml, then one tab per folder ("agents/ (6)", one
// per local skill, "contracts/ (2)", "evals/ (1)"), each opening a menu of its files with New
// file…, Rename… and Delete… (evals/ is read-only: "Edited from Jonah later"). A tablist with
// the arrow keys moving between tabs; the open file's tab is selected and controls the editor.
import { ChevronDown } from 'lucide-react';
import { type KeyboardEvent, type ReactNode, useId } from 'react';
import { cn } from '../../../ui/cn';
import { Menu, MenuContent, MenuGroup, MenuItem, MenuLabel, MenuSeparator, MenuTrigger } from '../../../ui/menu';
import { type FileGroup, factoryFile, groupOf } from './files';

interface FileTabsProps {
  groups: readonly FileGroup[];
  /** The open file. */
  path: string;
  /** The editor's id, which the selected tab controls. */
  panelId: string;
  onOpen: (path: string) => void;
  onNew: (group: FileGroup) => void;
  onRename: (path: string) => void;
  onDelete: (path: string) => void;
  /** Right of the tabs: Format and Copy. */
  actions?: ReactNode;
}

const tabClass =
  'relative inline-flex h-11 shrink-0 items-center gap-1 rounded-t-control px-3 text-sm whitespace-nowrap transition-colors duration-200 md:h-10 focus-visible:-outline-offset-2';

const fileName = (path: string) => path.split('/').at(-1) ?? path;

/** Arrow keys, Home and End move focus between the tabs (the selected one is the Tab stop). */
function moveFocus(e: KeyboardEvent<HTMLDivElement>) {
  const tabs = [...e.currentTarget.querySelectorAll<HTMLElement>('[role="tab"]')];
  const i = tabs.indexOf(document.activeElement as HTMLElement);
  const n = tabs.length;
  if (i < 0) return;
  const next =
    e.key === 'ArrowRight'
      ? (i + 1) % n
      : e.key === 'ArrowLeft'
        ? (i - 1 + n) % n
        : e.key === 'Home'
          ? 0
          : e.key === 'End'
            ? n - 1
            : undefined;
  if (next === undefined) return;
  e.preventDefault();
  tabs[next]?.focus();
}

export function FileTabs({ groups, path, panelId, onOpen, onNew, onRename, onDelete, actions }: FileTabsProps) {
  const current = groupOf(groups, path);
  const id = useId();
  return (
    <div className="flex min-w-0 flex-wrap items-end justify-between gap-x-3 gap-y-2 border-b border-line px-2 pt-2">
      <div
        role="tablist"
        aria-label="Files of the draft"
        onKeyDown={moveFocus}
        className="-mb-px flex min-w-0 flex-1 basis-[22rem] overflow-x-auto"
      >
        {groups.map((g) => {
          const selected = g === current;
          const tabId = `${id}-${g.id || 'other'}`;
          const shared = {
            id: tabId,
            role: 'tab' as const,
            'aria-selected': selected,
            'aria-controls': selected ? panelId : undefined,
            tabIndex: selected ? 0 : -1,
            className: cn(
              tabClass,
              selected
                ? 'border-b-2 border-ink font-medium text-ink'
                : 'border-b-2 border-transparent text-muted hover:text-ink',
            ),
          };
          if (g.id === factoryFile) {
            return (
              <button key={g.id} type="button" {...shared} onClick={() => onOpen(factoryFile)}>
                {factoryFile}
              </button>
            );
          }
          const open = selected && path !== factoryFile ? path : undefined;
          return (
            <Menu key={g.id}>
              <MenuTrigger asChild>
                <button type="button" {...shared}>
                  <span>{open ?? g.label}</span>
                  <ChevronDown size={14} strokeWidth={1.75} aria-hidden="true" />
                </button>
              </MenuTrigger>
              <MenuContent side="bottom" className="w-72">
                {g.files.length > 0 ? (
                  <MenuGroup aria-label={`Files in ${g.folder || 'the definition'}`}>
                    {g.files.map((f) => (
                      <MenuItem
                        key={f.path}
                        onSelect={() => onOpen(f.path)}
                        aria-current={f.path === path ? 'true' : undefined}
                        className={cn('font-mono text-[13px]', f.path === path && 'bg-shell')}
                      >
                        {f.path}
                      </MenuItem>
                    ))}
                  </MenuGroup>
                ) : (
                  <MenuLabel>No files yet</MenuLabel>
                )}
                {g.readOnly ? (
                  <MenuLabel>Edited from Jonah later</MenuLabel>
                ) : (
                  <>
                    <MenuSeparator />
                    <MenuItem onSelect={() => onNew(g)}>New file…</MenuItem>
                    {open ? (
                      <>
                        <MenuItem onSelect={() => onRename(open)}>Rename {fileName(open)}…</MenuItem>
                        <MenuItem onSelect={() => onDelete(open)} className="text-rework-ink">
                          Delete {fileName(open)}…
                        </MenuItem>
                      </>
                    ) : null}
                  </>
                )}
              </MenuContent>
            </Menu>
          );
        })}
      </div>
      {actions ? (
        <div className="flex items-center gap-2 pb-2 @max-[600px]:order-first @max-[600px]:w-full">{actions}</div>
      ) : null}
    </div>
  );
}
