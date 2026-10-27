import { Check } from 'lucide-react';
import { DropdownMenu as M } from 'radix-ui';
import type { ComponentProps } from 'react';
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

/** The white panel beside its trigger: to the right of the sidebar, below a phone's top row. */
export function MenuContent({
  className,
  side = 'right',
  align = 'start',
  sideOffset = 6,
  ...props
}: ComponentProps<typeof M.Content>) {
  return (
    <M.Portal>
      <M.Content
        side={side}
        align={align}
        sideOffset={sideOffset}
        collisionPadding={8}
        className={cn(
          'z-50 flex w-80 max-w-[calc(100vw-16px)] flex-col overflow-y-auto rounded-card border border-rule bg-card p-2 text-ink shadow-pop outline-none',
          'max-h-[var(--radix-dropdown-menu-content-available-height)] data-[state=open]:animate-[fade-in_150ms_ease-out]',
          className,
        )}
        {...props}
      />
    </M.Portal>
  );
}

/** A row of a menu or of a sheet's list: 44 px, an 18 px icon, the label. */
export const menuRow =
  'flex min-h-11 w-full items-center gap-3 rounded-control px-2.5 text-left text-[15px] text-ink no-underline transition-colors duration-200';

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
        'grid min-h-[52px] cursor-pointer grid-cols-[36px_minmax(0,1fr)_20px] items-center gap-3 rounded-control px-2 py-1.5 text-ink transition-colors duration-200 focus-visible:-outline-offset-2 data-[highlighted]:bg-shell data-[state=checked]:bg-shell',
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
  return <M.Label className={cn('mx-2.5 mt-2.5 mb-1 text-[13px] font-medium text-muted', className)} {...props} />;
}

export function MenuSeparator({ className, ...props }: ComponentProps<typeof M.Separator>) {
  return <M.Separator className={cn('my-2 h-px bg-line', className)} {...props} />;
}
