import { minutesAndSeconds, useSecondsLeft } from '../ui/countdown';

/**
 * "Send a new code": off with its countdown ("Send a new code in 0:24") until `at`, then a link
 * styled button. `at` null: enabled now.
 */
export function ResendLink({ at, pending, onResend }: { at: number | null; pending: boolean; onResend: () => void }) {
  const left = useSecondsLeft(at);
  const waiting = left > 0;
  return (
    <button
      type="button"
      disabled={waiting || pending}
      onClick={onResend}
      className="inline-flex min-h-11 items-center border-0 bg-transparent p-0 text-left md:min-h-6 text-sm text-ink underline underline-offset-[3px] transition-colors duration-200 hover:text-run disabled:cursor-not-allowed disabled:text-muted disabled:no-underline"
    >
      {waiting ? `Send a new code in ${minutesAndSeconds(left)}` : pending ? 'Sending a new code…' : 'Send a new code'}
    </button>
  );
}
