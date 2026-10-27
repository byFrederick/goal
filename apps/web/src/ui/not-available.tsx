// A control whose feature is not built yet (ADR 0016, decision 4). It stays in the tab order with
// aria-disabled, so keyboard users reach the tooltip; it never runs the control's own action and never
// fakes a result. The item that builds the feature takes this wrapper off the control.
import { cloneElement, type KeyboardEvent, type MouseEvent, type ReactElement } from 'react';
import { Tooltip } from './tooltip';

interface NotAvailableProps {
  /** What the control is. Shown as the tip's first line; "Not available yet" becomes the second. Icon-only controls need it. */
  label?: string;
  /** One focusable control: a button or a link. Its own onClick is never called. */
  children: ReactElement<{
    onClick?: (event: MouseEvent<HTMLElement>) => void;
    onKeyDown?: (event: KeyboardEvent<HTMLElement>) => void;
    'aria-disabled'?: boolean | 'true' | 'false';
  }>;
}

/** Enter and Space activate a button or a link; blocking them here keeps the control inert. */
const activationKeys = new Set(['Enter', ' ']);

export function NotAvailable({ label, children }: NotAvailableProps) {
  const control = cloneElement(children, {
    'aria-disabled': 'true',
    onClick: (event: MouseEvent<HTMLElement>) => {
      // A link would navigate and a submit button would submit: the default is stopped too.
      event.preventDefault();
    },
    onKeyDown: (event: KeyboardEvent<HTMLElement>) => {
      if (activationKeys.has(event.key)) {
        event.preventDefault();
        return;
      }
      children.props.onKeyDown?.(event);
    },
  });
  return label ? (
    <Tooltip label={label} detail="Not available yet">
      {control}
    </Tooltip>
  ) : (
    <Tooltip label="Not available yet">{control}</Tooltip>
  );
}
