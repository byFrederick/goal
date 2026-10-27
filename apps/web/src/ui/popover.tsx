import { Popover as P } from 'radix-ui';
import type { ComponentProps } from 'react';
import { cn } from './cn';

// A panel beside its trigger that holds a few controls (the account): role="dialog", Tab moves
// between its controls, Escape or a click outside closes it and focus returns to the trigger.
export const Popover = P.Root;
export const PopoverTrigger = P.Trigger;
export const PopoverClose = P.Close;

/** The white panel, like a menu's: to the right of the rail, below a phone's top row. */
export function PopoverContent({
  className,
  side = 'right',
  align = 'end',
  sideOffset = 6,
  ...props
}: ComponentProps<typeof P.Content>) {
  return (
    <P.Portal>
      <P.Content
        side={side}
        align={align}
        sideOffset={sideOffset}
        collisionPadding={8}
        className={cn(
          'z-50 flex w-80 max-w-[calc(100vw-16px)] flex-col overflow-y-auto rounded-card border border-rule bg-card p-2 text-ink shadow-pop outline-none',
          'max-h-[var(--radix-popover-content-available-height)] data-[state=open]:animate-[fade-in_150ms_ease-out]',
          className,
        )}
        {...props}
      />
    </P.Portal>
  );
}
