// A secret field's input: a password input that is never filled from anywhere (new-password,
// since browsers ignore autocomplete=off on a password input and would offer a saved one; no
// value from goald, which never answers one), with Show to check what was pasted.
import { type ComponentProps, useState } from 'react';
import { cn } from '../../ui/cn';
import { Input } from '../../ui/field';

// Show is drawn as a 32 px chip; below md the button around it is a 44 × 44 px box, so the hit area is real.

export function SecretInput({ className, ...props }: Omit<ComponentProps<typeof Input>, 'type' | 'autoComplete'>) {
  const [shown, setShown] = useState(false);
  return (
    <span className="relative flex min-w-0">
      <Input
        {...props}
        type={shown ? 'text' : 'password'}
        autoComplete="new-password"
        autoCapitalize="off"
        spellCheck={false}
        data-1p-ignore=""
        className={cn('pr-16 font-mono md:text-sm', className)}
      />
      <button
        type="button"
        aria-pressed={shown}
        aria-label="Show the value"
        onClick={() => setShown((s) => !s)}
        className={cn(
          'group absolute top-1/2 right-0 inline-flex -translate-y-1/2 items-center justify-center text-meta font-control text-ink-soft max-md:min-h-11 max-md:min-w-11 md:right-1',
        )}
      >
        <span className="inline-flex h-8 items-center justify-center rounded-chip bg-hover px-2 transition-colors duration-(--duration-quick) group-hover:text-ink max-md:mr-1">
          {shown ? 'Hide' : 'Show'}
        </span>
      </button>
    </span>
  );
}
