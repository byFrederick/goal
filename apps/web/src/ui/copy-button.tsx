// Copies text to the clipboard. The icon swaps to a check and back after 1.5 s (MOTION.md, Copy to
// clipboard: icon-swap 09). "Copied" is announced through a live region; the name stays the label.
import { Check, Copy } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { Button, type ButtonProps } from './button';

const RESET_AFTER = 1500;

type CopyButtonProps = Omit<ButtonProps, 'children' | 'onClick'> & {
  /** What goes on the clipboard. */
  text: string;
  /** The visible text and, unless aria-label is given, the accessible name. */
  label?: string;
  /** Called when the clipboard refuses, so the caller can select the text by hand. */
  onFailed?: () => void;
  onClick?: ButtonProps['onClick'];
};

export function CopyButton({ text, label = 'Copy', onFailed, onClick, size = 'sm', ...props }: CopyButtonProps) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(timer.current), []);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      setCopied(false);
      onFailed?.();
      return;
    }
    setCopied(true);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setCopied(false), RESET_AFTER);
  };

  return (
    <>
      <Button
        variant="secondary"
        {...({ ...props, size } as ButtonProps)}
        onClick={(event) => {
          onClick?.(event);
          // A control that is not available (NotAvailable) copies nothing.
          if (props['aria-disabled'] === true || props['aria-disabled'] === 'true') return;
          void copy();
        }}
      >
        <span
          key={String(copied)}
          data-testid={copied ? 'copy-check' : 'copy-icon'}
          className="inline-flex motion-safe:animate-icon-swap motion-reduce:animate-none"
        >
          {copied ? (
            <Check size={14} strokeWidth={1.5} aria-hidden="true" />
          ) : (
            <Copy size={14} strokeWidth={1.5} aria-hidden="true" />
          )}
        </span>
        {label}
      </Button>
      <span aria-live="polite" className="sr-only" data-testid="copy-status">
        {copied ? 'Copied' : ''}
      </span>
    </>
  );
}
