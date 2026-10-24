// A labelled form control (C §10.11 `Field`): the label, the control, a help line and an error,
// wired for screen readers. The control inside (`Input`) takes its id, aria-describedby and
// aria-invalid from the Field around it.
import { type ComponentProps, createContext, type ReactNode, use, useId } from 'react';
import { cn } from './cn';

interface FieldControl {
  id: string;
  describedBy: string | undefined;
  invalid: boolean;
}

const FieldContext = createContext<FieldControl | null>(null);

interface FieldProps {
  label: ReactNode;
  /** Under the control, always shown. */
  help?: ReactNode;
  /** Under the control; marks it invalid. */
  error?: string | undefined;
  /** Layout: a column by default; a grid lays the label beside the control (help and error span it). */
  className?: string;
  children: ReactNode;
}

export function Field({ label, help, error, className, children }: FieldProps) {
  const id = useId();
  const helpId = `${id}-help`;
  const errorId = `${id}-error`;
  const describedBy = [error ? errorId : null, help ? helpId : null].filter(Boolean).join(' ') || undefined;

  return (
    <div className={cn('flex min-w-0 flex-col gap-1.5', className)}>
      <label htmlFor={id} className="text-sm font-medium text-ink">
        {label}
      </label>
      <FieldContext value={{ id, describedBy, invalid: Boolean(error) }}>{children}</FieldContext>
      {error ? (
        <p id={errorId} className="col-span-full m-0 text-[13px] leading-snug text-rework-ink">
          {error}
        </p>
      ) : null}
      {help ? (
        <div id={helpId} className="col-span-full m-0 text-[13px] leading-snug text-muted">
          {help}
        </div>
      ) : null}
    </div>
  );
}

/** A text input: 44 px tall on phones (16 px text, so iOS does not zoom), 40 px from 768 px. */
export function Input({ className, ...props }: ComponentProps<'input'>) {
  const field = use(FieldContext);
  return (
    <input
      id={field?.id}
      aria-describedby={field?.describedBy}
      aria-invalid={field?.invalid || undefined}
      className={cn(
        'h-11 w-full min-w-0 rounded-control border border-rule bg-surface px-3 text-base text-ink transition-colors duration-200 md:h-10 md:text-[15px]',
        'hover:border-faint aria-[invalid=true]:border-rework',
        className,
      )}
      {...props}
    />
  );
}
