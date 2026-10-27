import { Tooltip as T } from 'radix-ui';
import type { ReactElement, ReactNode } from 'react';

/** Shared timing for every tooltip: 300 ms on hover, at once when moving between triggers. */
export function TooltipProvider({ children }: { children: ReactNode }) {
  return (
    <T.Provider delayDuration={300} skipDelayDuration={200}>
      {children}
    </T.Provider>
  );
}

interface TooltipProps {
  /** The trigger: a focusable element whose accessible name already says everything the tip shows. */
  children: ReactElement;
  /** The first line: what the trigger is. */
  label: ReactNode;
  /** A muted second line: its status or what it does. */
  detail?: ReactNode | undefined;
  side?: 'right' | 'top' | 'bottom' | 'left';
}

/**
 * A white tip beside a trigger (Radix: shown on hover and on keyboard focus, dismissed by
 * Escape). It repeats the trigger's name visually and never carries anything the name lacks.
 */
export function Tooltip({ children, label, detail, side = 'right' }: TooltipProps) {
  return (
    <T.Root>
      {/* The trigger's name already says what the tip shows: no duplicate description. */}
      <T.Trigger asChild aria-describedby={undefined}>
        {children}
      </T.Trigger>
      <T.Portal>
        <T.Content
          side={side}
          sideOffset={10}
          aria-hidden="true"
          className="z-50 rounded-chip border border-rule bg-card px-2.5 py-1.5 text-[13px] leading-[1.35] font-medium whitespace-nowrap text-ink shadow-pop data-[state=delayed-open]:animate-[fade-in_150ms_ease-out]"
        >
          {label}
          {detail ? <span className="block text-xs font-normal text-muted">{detail}</span> : null}
        </T.Content>
      </T.Portal>
    </T.Root>
  );
}
