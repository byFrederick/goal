import { ChevronDown } from 'lucide-react';
import { Collapsible as C } from 'radix-ui';
import { type ReactNode, useId, useState } from 'react';

interface DisclosureProps {
  /** The text of the button that opens and closes the section. */
  summary: ReactNode;
  children: ReactNode;
  defaultOpen?: boolean;
  /** Controls the open state; leave it out to let the disclosure keep its own. */
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  className?: string;
}

/**
 * A section that opens under its summary (Radix Collapsible, MOTION.md's accordion row). The summary is a
 * button with aria-expanded and controls the content. The content grows from 0 to its height, and closed
 * content leaves the tab order and the accessibility tree. Tool-call details and settings groups use it.
 */
export function Disclosure({ summary, children, defaultOpen = false, open, onOpenChange, className }: DisclosureProps) {
  const [own, setOwn] = useState(defaultOpen);
  const isOpen = open ?? own;
  const contentId = useId();
  return (
    <C.Root
      open={isOpen}
      onOpenChange={(next) => {
        setOwn(next);
        onOpenChange?.(next);
      }}
      className={className}
    >
      <C.Trigger
        aria-controls={contentId}
        className="motion-disclosure-trigger flex min-h-11 w-full cursor-pointer items-center justify-between gap-3 rounded-control px-2 text-left text-body text-ink"
      >
        {summary}
        <ChevronDown className="motion-chevron" size={16} strokeWidth={1.75} aria-hidden="true" />
      </C.Trigger>
      {/* Not Collapsible.Content: Radix hides it with the hidden attribute, which ends the close at once. */}
      <div
        id={contentId}
        data-state={isOpen ? 'open' : 'closed'}
        aria-hidden={isOpen ? undefined : true}
        inert={!isOpen}
        className="motion-disclosure"
      >
        <div>{children}</div>
      </div>
    </C.Root>
  );
}
