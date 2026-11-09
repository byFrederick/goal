// A change goald refused because it was starting is never sent again by itself: the person sees that
// Goal is restarting and sends it again (the page's own error message stays beside this toast).
import { toast } from 'sonner';
import { restarting } from '../shared/code-sign-in';
import { isStarting } from './retry';

/** The MutationCache's onError: one toast when the failed write was refused by a restarting goald. */
export function notifyRestarting(error: unknown): void {
  if (isStarting(error)) toast.error(restarting, { id: 'goal-restarting' });
}
