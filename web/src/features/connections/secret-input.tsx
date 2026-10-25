// A secret field's input: a password input that is never filled from anywhere (new-password,
// since browsers ignore autocomplete=off on a password input and would offer a saved one; no
// value from goald, which never answers one), with Show to check what was pasted.
import { Eye, EyeOff } from 'lucide-react';
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
        className={cn('pr-12 font-mono md:text-sm', className)}
      />
      <button
        type="button"
        aria-pressed={shown}
        aria-label="Show the value"
        onClick={() => setShown((s) => !s)}
        className="absolute top-0 right-0 inline-flex size-11 items-center justify-center rounded-control text-muted transition-colors duration-200 hover:text-ink md:size-10"
      >
        {shown ? (
          <EyeOff size={17} strokeWidth={1.5} aria-hidden="true" />
        ) : (
          <Eye size={17} strokeWidth={1.5} aria-hidden="true" />
        )}
      </button>
    </span>
  );
}
