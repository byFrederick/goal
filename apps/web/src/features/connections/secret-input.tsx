// A secret field's input: a password input that is never filled from anywhere (new-password,
// since browsers ignore autocomplete=off on a password input and would offer a saved one; no
// value from goald, which never answers one), with Show to check what was pasted.
import { type ComponentProps, useState } from 'react';
import { cn } from '../../ui/cn';
import { Input } from '../../ui/field';

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
        className="absolute top-1/2 right-1.5 inline-flex h-8 -translate-y-1/2 items-center justify-center rounded-chip bg-hover px-2 text-meta font-control text-ink-soft transition-colors duration-(--duration-quick) hover:text-ink"
      >
        {shown ? 'Hide' : 'Show'}
      </button>
    </span>
  );
}
