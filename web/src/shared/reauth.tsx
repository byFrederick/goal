// Confirm it's you (canvas "Sign-In-Reauth"; spec 3 §10.1): some changes need a sign-in from the
// last 10 minutes, and goald answers 403 reauth-required to an older session. The API client
// (session.ts) then asks this host, which opens the dialog; once the person signed in again by
// code the request is sent once more. Google leaves the page: back on it, a toast says to try
// once more (the action is not repeated on its own). The dialog's code loads only when it opens.
import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { setSessionHandlers } from '../api/session';

const ReauthDialog = lazy(() => import('./reauth-dialog'));

/** Set while the browser is away at Google to sign in again; read once back on the page. */
export const reauthReturnKey = 'goal.reauth';

export function ReauthHost() {
  const [open, setOpen] = useState(false);
  // Every request refused while the dialog is open waits for the same answer.
  const waiting = useRef<((signedIn: boolean) => void)[]>([]);

  useEffect(() => {
    setSessionHandlers({
      reauth: () =>
        new Promise<boolean>((resolve) => {
          waiting.current.push(resolve);
          setOpen(true);
        }),
    });
    try {
      if (sessionStorage.getItem(reauthReturnKey)) {
        sessionStorage.removeItem(reauthReturnKey);
        toast.success('Signed in again. Try that once more.');
      }
    } catch {
      // Storage refused (a private window): there is no toast to show.
    }
  }, []);

  const done = (signedIn: boolean) => {
    setOpen(false);
    for (const resolve of waiting.current.splice(0)) resolve(signedIn);
  };

  return open ? (
    <Suspense fallback={null}>
      <ReauthDialog onDone={done} />
    </Suspense>
  ) : null;
}
