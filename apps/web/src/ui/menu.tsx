import { Check, ChevronRight } from 'lucide-react';
import { DropdownMenu as M } from 'radix-ui';
import { type ComponentProps, useRef } from 'react';
import { cn } from './cn';

// A menu of actions or links on Radix DropdownMenu (C §10.11): role="menu", arrow keys and
// typeahead between items, Escape closes it and returns focus to the trigger.

/**
 * Not modal: a menu (unlike a dialog) leaves the page in the accessibility tree. A modal Radix
 * menu hides the page with aria-hidden while its links stay focusable (axe aria-hidden-focus);
 * outside clicks and Escape still close it, and Tab cannot leave it.
 */
export function Menu(props: Omit<ComponentProps<typeof M.Root>, 'modal'>) {
  return <M.Root modal={false} {...props} />;
}
export const MenuTrigger = M.Trigger;
export const MenuGroup = M.Group;
export const MenuRadioGroup = M.RadioGroup;
export const MenuSub = M.Sub;

/** The white panel beside its trigger: to the right of the sidebar, below a phone's top row. */
export function MenuContent({
  className,
  side = 'right',
  align = 'start',
  sideOffset = 6,
  onInteractOutside,
  ref,
  ...props
}: ComponentProps<typeof M.Content>) {
  const node = useRef<HTMLDivElement | null>(null);
  return (
    <M.Portal>
      <M.Content
        ref={(el) => {
          node.current = el;
          if (typeof ref === 'function') ref(el);
          else if (ref) ref.current = el;
        }}
        side={side}
        align={align}
        sideOffset={sideOffset}
        collisionPadding={8}
        // A press on the menu's own trigger is never an outside press: React re-renders the menu open before the
        // document's pointerdown listener runs, so the trigger's toggle would be followed by a dismiss. The trigger
        // opens or closes the menu itself (MOTION.md rule 7).
        onInteractOutside={(event) => {
          const trigger = props['aria-labelledby'] ?? node.current?.getAttribute('aria-labelledby');
          const target = event.target instanceof Element ? event.target : null;
          if (trigger && target?.closest(`[id="${CSS.escape(trigger)}"]`)) event.preventDefault();
          else if (node.current?.dataset.state === 'closed') event.preventDefault();
          else onInteractOutside?.(event);
        }}
        className={cn(
          'motion-panel motion-menu z-50 flex w-80 max-w-[calc(100vw-16px)] flex-col overflow-y-auto rounded-card bg-card p-1 text-ink shadow-pop outline-none',
          'max-h-[var(--radix-dropdown-menu-content-available-height)]',
          className,
        )}
        {...props}
      />
    </M.Portal>
  );
}

/**
 * A row of a menu or of a sheet's list: 44 px on a phone, 30 px from 768 px up, an 18 px icon, the label.
 * The row is the same on a sheet's list, so both take the phone's touch target.
 */
export const menuRow =
  'flex min-h-11 md:min-h-7.5 w-full items-center gap-3 rounded-control px-2.5 text-left text-body text-ink no-underline transition-colors duration-quick';

/**
 * One action. With `asChild` it wraps a Link (a menu link). A disabled item (a screen a later
 * phase builds) is skipped by the arrow keys and muted.
 */
export function MenuItem({ className, ...props }: ComponentProps<typeof M.Item>) {
  return (
    <M.Item
      className={cn(
        menuRow,
        'cursor-pointer focus-visible:-outline-offset-2 data-[disabled]:cursor-default data-[disabled]:text-muted data-[highlighted]:bg-shell',
        className,
      )}
      {...props}
    />
  );
}

/** A choice of one among several (the organizations); the checked one shows a check. */
export function MenuRadioItem({ className, children, ...props }: ComponentProps<typeof M.RadioItem>) {
  return (
    <M.RadioItem
      className={cn(
        'grid min-h-[52px] cursor-pointer grid-cols-[36px_minmax(0,1fr)_20px] items-center gap-3 rounded-control px-2 py-1.5 text-ink transition-colors duration-quick focus-visible:-outline-offset-2 data-[highlighted]:bg-shell data-[state=checked]:bg-shell',
        className,
      )}
      {...props}
    >
      {children}
      <M.ItemIndicator className="flex">
        <Check size={18} strokeWidth={1.75} aria-hidden="true" />
      </M.ItemIndicator>
    </M.RadioItem>
  );
}

/** A plain choice of one (a filter's values): its label, and a check on the chosen one. */
export function MenuChoice({ className, children, ...props }: ComponentProps<typeof M.RadioItem>) {
  return (
    <M.RadioItem
      className={cn(
        menuRow,
        'cursor-pointer justify-between focus-visible:-outline-offset-2 data-[highlighted]:bg-shell',
        className,
      )}
      {...props}
    >
      {children}
      <M.ItemIndicator className="flex">
        <Check size={18} strokeWidth={1.75} aria-hidden="true" />
      </M.ItemIndicator>
    </M.RadioItem>
  );
}

/** A group's heading ("Organizations", "Acme"). Give it an id and label the group with it. */
export function MenuLabel({ className, ...props }: ComponentProps<typeof M.Label>) {
  return <M.Label className={cn('mx-2.5 mt-2.5 mb-1 text-secondary font-medium text-muted', className)} {...props} />;
}

export function MenuSeparator({ className, ...props }: ComponentProps<typeof M.Separator>) {
  return <M.Separator className={cn('my-2 h-px bg-line', className)} {...props} />;
}

/** A row that opens a submenu to its side (a phone's More actions lists a page's filter menus this way). */
export function MenuSubTrigger({ className, children, ...props }: ComponentProps<typeof M.SubTrigger>) {
  return (
    <M.SubTrigger
      className={cn(
        menuRow,
        'cursor-pointer justify-between focus-visible:-outline-offset-2 data-[highlighted]:bg-shell data-[state=open]:bg-shell',
        className,
      )}
      {...props}
    >
      {children}
      <ChevronRight size={16} strokeWidth={1.75} aria-hidden="true" />
    </M.SubTrigger>
  );
}

/** The panel of a submenu, drawn like MenuContent. */
export function MenuSubContent({ className, sideOffset = 4, ...props }: ComponentProps<typeof M.SubContent>) {
  return (
    <M.Portal>
      <M.SubContent
        sideOffset={sideOffset}
        collisionPadding={8}
        className={cn(
          'motion-panel motion-menu z-50 flex w-64 max-w-[calc(100vw-16px)] flex-col overflow-y-auto rounded-card bg-card p-1 text-ink shadow-pop outline-none',
          className,
        )}
        {...props}
      />
    </M.Portal>
  );
}
