// The Activity card's sentences (Floor, Activity): one line per milestone event, the item's ref in
// bold and the rest in words. The actor is named by its role ("frontend-engineer"), never by its copy
// number ("frontend-engineer-2"); the engine and a missing actor name nobody. A kind this build does not
// know reads as its kind, so a new event never breaks the card.
import type { ActivityEvent } from '../../api/types';
import { humanize } from '../../shared/format';

/** The role an actor acts as: its name without a copy number, or null for the engine or no actor. */
export function actorRole(actor: string): string | null {
  if (actor === '' || actor === 'engine') return null;
  return actor.replace(/-\d+$/, '');
}

/** The sentence of one event: the bold head (the ref and its verb) and the words after it. */
export function activityWords(event: ActivityEvent): { head: string; rest: string } {
  const station = event.station ? humanize(event.station) : '';
  const role = actorRole(event.actor);
  const by = role ? ` by ${role}` : '';
  const at = station ? ` at ${station}` : '';
  switch (event.kind) {
    case 'created':
      return { head: `${event.ref} created`, rest: `${by}${at}` };
    case 'moved': {
      const to = typeof event.data.to === 'string' ? ` to ${humanize(event.data.to)}` : at;
      return { head: `${event.ref} moved`, rest: `${by}${to}` };
    }
    case 'gate_approved':
      return { head: `${event.ref} approved`, rest: `${by}${at}` };
    case 'gate_rejected':
    case 'rejected':
      return { head: `${event.ref} sent back`, rest: `${by}${at}` };
    case 'approved':
      return { head: `${event.ref} approved`, rest: by };
    case 'blocked':
      return { head: `${event.ref} blocked`, rest: `${by}${at}` };
    case 'check_failed':
      return { head: `${event.ref} failed a check`, rest: `${by}${at}` };
    case 'merged':
      return { head: `${event.ref} merged`, rest: `${by}${at}` };
    case 'done':
      return { head: `${event.ref} done`, rest: at };
    case 'assembled':
      return { head: `${event.ref} assembled from its parts`, rest: by };
    case 'cancelled':
      return { head: `${event.ref} cancelled`, rest: by };
    default:
      return { head: `${event.ref} ${humanize(event.kind).toLowerCase()}`, rest: `${by}${at}` };
  }
}

/** The avatar's letters for an actor's role: "Rv" for reviewer, "Fe" for frontend-engineer, "Goal" for the engine. */
export function avatarOf(actor: string): string {
  const role = actorRole(actor);
  if (!role) return 'G';
  const words = role.split(/[-_]+/).filter(Boolean);
  return words.length > 1
    ? `${words[0]?.[0] ?? ''}${words[1]?.[0] ?? ''}`.toUpperCase()
    : role.slice(0, 2).toUpperCase();
}
