// The keyboard shortcuts list (docs/design/screens/account/shortcuts.html): every shortcut the app binds, in
// groups. It opens on ? and from the account menu's Keyboard shortcuts entry. Opened by the key it has no
// transition (MOTION.md, rule 1: a keyboard action never animates); opened from the menu it uses the dialog row.
// The list names only what is bound today. The export also shows G-then-H/F/B/I, X, E and ⌘S, which nothing binds
// yet, so they are left out until they are (the spec of this part).
import { useEffect } from 'react';
import { Dialog, DialogContent } from '../ui/dialog';

/** A key the app binds: each cap is one key, and the modifiers come first. */
interface Shortcut {
  label: string;
  keys: string[];
}

interface Group {
  label: string;
  shortcuts: Shortcut[];
}

/** Whether the platform names the ⌘ modifier (macOS and iOS); elsewhere the shortcut says Ctrl. */
function isApplePlatform(): boolean {
  return typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/.test(navigator.platform);
}

/** The bound shortcuts, grouped as the export shows them. Each entry is bound in the code named in its comment. */
function groupsFor(apple: boolean): Group[] {
  const mod = apple ? '⌘' : 'Ctrl';
  const shift = apple ? '⇧' : 'Shift';
  return [
    {
      label: 'Global',
      shortcuts: [
        // sidebar-search.tsx, useSearchShortcut
        { label: 'Search', keys: [mod, 'K'] },
        // shared/new-item.tsx, NewItemFromSidebar
        { label: 'New item', keys: ['C'] },
        // this file, useShortcutsKey
        { label: 'Keyboard shortcuts', keys: ['?'] },
      ],
    },
    {
      label: 'Lists',
      shortcuts: [
        // features/needs-you/needs-you-page.tsx
        { label: 'Next and previous row', keys: ['J', 'K'] },
        // shared/move-menu.tsx
        { label: 'Move the item to another column', keys: ['M'] },
      ],
    },
    {
      label: 'Needs you',
      shortcuts: [
        // features/needs-you/needs-you-page.tsx
        { label: 'Approve', keys: ['A'] },
        { label: 'Deny', keys: ['D'] },
      ],
    },
    {
      label: 'Editor',
      shortcuts: [
        // features/factory-editor/frame/editor-frame.tsx, useUndoKeys
        { label: 'Undo', keys: [mod, 'Z'] },
        { label: 'Redo', keys: [mod, shift, 'Z'] },
      ],
    },
  ];
}

/** Whether a key from `target` is typed into a field, where ? is a character (needs-you-page.tsx has the same check). */
function isTyping(target: EventTarget | null): boolean {
  return (
    target instanceof HTMLElement && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))
  );
}

/**
 * Calls `open` when ? is pressed: Shift+/ on most layouts, with no Ctrl, Meta or Alt, not held down, and not
 * in a field or inside a dialog or menu (a key there belongs to that control).
 */
export function useShortcutsKey(open: () => void) {
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.key !== '?' || e.ctrlKey || e.metaKey || e.altKey || e.repeat) return;
      if (isTyping(e.target)) return;
      if (e.target instanceof Element && e.target.closest('[role="dialog"], [role="menu"], [role="listbox"]')) return;
      e.preventDefault();
      open();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [open]);
}

interface ShortcutsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Opened by key: no transition on open or close. */
  instant: boolean;
}

export function ShortcutsDialog({ open, onOpenChange, instant }: ShortcutsDialogProps) {
  const groups = groupsFor(isApplePlatform());
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title="Keyboard shortcuts" instant={instant}>
        <div className="flex flex-col gap-4">
          {groups.map((group) => {
            const id = `shortcuts-${group.label.toLowerCase().replace(/\s+/g, '-')}`;
            return (
              <section key={group.label} aria-labelledby={id} className="flex flex-col gap-1">
                <h3 id={id} className="m-0 text-meta font-semibold text-muted">
                  {group.label}
                </h3>
                <ul className="m-0 flex list-none flex-col p-0">
                  {group.shortcuts.map((shortcut) => (
                    <li key={shortcut.label} className="flex min-h-9 items-center justify-between gap-4 text-body">
                      <span>{shortcut.label}</span>
                      <span className="flex flex-none items-center gap-1">
                        {shortcut.keys.map((key) => (
                          <kbd
                            key={key}
                            className="grid h-5 min-w-5 place-items-center rounded-chip border border-rule px-1 font-sans text-meta font-semibold text-ink-soft"
                          >
                            {key}
                          </kbd>
                        ))}
                      </span>
                    </li>
                  ))}
                </ul>
              </section>
            );
          })}
        </div>
      </DialogContent>
    </Dialog>
  );
}
