import { Tooltip as T } from 'radix-ui';
import type { ReactElement, ReactNode } from 'react';
import { cn } from './cn';

// Mirrors --duration-micro in styles.css (MOTION.md, Tooltip): the wait before the first tip opens.
// tooltip.test.tsx checks the two agree. Radix takes milliseconds, so the value cannot be a token.
export const OPEN_DELAY_MS = 80;
// How long after one tip closes the next opens with no delay: a toolbar's tips open at once.
const SKIP_DELAY_MS = 300;

/** Shared timing for every tooltip: 80 ms on hover, at once when moving between triggers. */
export function TooltipProvider({ children }: { children: ReactNode }) {
  return (
    <T.Provider delayDuration={OPEN_DELAY_MS} skipDelayDuration={SKIP_DELAY_MS}>
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
  /** Wraps a long explanation in a tip at most 18rem wide instead of one line. */
  wrap?: boolean;
}

/**
 * A white tip beside a trigger (Radix: shown on hover and on keyboard focus, dismissed by
 * Escape). It repeats the trigger's name visually and never carries anything the name lacks. It
 * fades in and grows from its trigger; it closes at once (MOTION.md, Tooltip).
 */
export function Tooltip({ children, label, detail, side = 'right', wrap = false }: TooltipProps) {
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
          className={cn(
            'z-50 origin-(--radix-tooltip-content-transform-origin) rounded-chip border border-rule bg-card px-2.5 py-1.5 text-[13px] leading-[1.35] font-medium text-ink shadow-pop data-[state=delayed-open]:motion-safe:animate-tooltip-in data-[state=delayed-open]:motion-reduce:animate-fade-in data-[state=instant-open]:motion-safe:animate-tooltip-in data-[state=instant-open]:motion-reduce:animate-fade-in',
            wrap ? 'max-w-72 whitespace-normal' : 'whitespace-nowrap',
          )}
        >
          {label}
          {detail ? <span className="block text-xs font-normal text-muted">{detail}</span> : null}
        </T.Content>
      </T.Portal>
    </T.Root>
  );
}
