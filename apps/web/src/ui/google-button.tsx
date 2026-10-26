import { Button } from './button';
import { Spinner } from './spinner';

/**
 * "Continue with Google", with Google's own mark (a file of the app: its brand colours stay out of
 * the components). Busy while the app leaves for Google.
 */
export function GoogleButton({ pending, onClick }: { pending: boolean; onClick: () => void }) {
  return (
    <Button
      variant="white"
      size="lg"
      aria-busy={pending || undefined}
      onClick={() => {
        if (!pending) onClick();
      }}
      className="h-12 w-full justify-center aria-busy:cursor-progress"
    >
      {pending ? <Spinner tone="dark" /> : <img src="/google-g.svg" alt="" width={18} height={18} />}
      Continue with Google
    </Button>
  );
}
