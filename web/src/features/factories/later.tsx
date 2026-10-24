// Entry points to screens a later phase builds (the New factory wizard, a factory's Definition):
// shown so the page has its final shape, marked "Later", going nowhere (app/screens.ts says which).
import type { LucideIcon } from 'lucide-react';
import { laterText } from '../../app/screens';
import { Button, type ButtonProps } from '../../ui/button';

/**
 * A button that does nothing yet. aria-disabled, not disabled: it stays in the tab order, and its
 * visible "Later" is part of its name, so everyone learns why it does nothing.
 */
export function LaterButton({
  label,
  icon: Icon,
  variant = 'secondary',
  size = 'md',
}: {
  label: string;
  icon?: LucideIcon;
  variant?: NonNullable<ButtonProps['variant']>;
  size?: NonNullable<ButtonProps['size']>;
}) {
  return (
    <Button variant={variant} size={size} aria-disabled="true">
      {Icon ? <Icon size={16} strokeWidth={1.5} aria-hidden="true" /> : null}
      {label}
      <span className="text-xs font-normal">{laterText}</span>
    </Button>
  );
}
