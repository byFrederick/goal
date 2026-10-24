// The Organization settings form (C §10.9): what it holds, the rules it checks before saving
// (goald's own, so a request is never refused for a reason the form could have said), the patch
// that sends only what changed, and the sentence under the slot limits.
import type { OrgSettings, OrgSettingsPatch } from '../../api/types';
import { checkSlug } from '../../shared/org-rules';

/** The form's values as typed; a slot cap is text, blank for no cap. */
export interface SettingsForm {
  name: string;
  mark: string;
  slug: string;
  /** The organization's cap: agents running at once across its factories. */
  max: string;
  /** Each factory's cap by factory id. */
  factories: Record<string, string>;
}

/** An error per field; a factory's cap is `factory:<id>`. */
export type FieldErrors = Partial<Record<'name' | 'mark' | 'slug' | 'max' | `factory:${string}`, string>>;

const capText = (cap: number | null) => (cap === null ? '' : String(cap));

export function formOf(settings: OrgSettings): SettingsForm {
  return {
    name: settings.name,
    mark: settings.mark,
    slug: settings.slug,
    max: capText(settings.slots.max),
    factories: Object.fromEntries(settings.slots.factories.map((f) => [f.factory_id, capText(f.max)])),
  };
}

const maxCap = 1000;

/** A cap as typed: null when blank (no cap), the number when it is 1 to 1000, undefined otherwise. */
export function parseCap(text: string): number | null | undefined {
  const t = text.trim();
  if (t === '') return null;
  if (!/^\d+$/.test(t)) return undefined;
  const n = Number(t);
  return n >= 1 && n <= maxCap ? n : undefined;
}

const capError = `A whole number from 1 to ${maxCap}, or blank for no limit.`;

export function validate(form: SettingsForm): FieldErrors {
  const errors: FieldErrors = {};
  const name = form.name.trim();
  if (!name) errors.name = 'Give the organization a name.';
  else if ([...name].length > 80) errors.name = 'At most 80 characters.';
  const mark = form.mark.trim();
  if (!mark || [...mark].length > 2) errors.mark = 'One or two letters or digits.';
  const slug = checkSlug(form.slug.trim());
  if (slug) errors.slug = slug;
  if (parseCap(form.max) === undefined) errors.max = capError;
  for (const [id, text] of Object.entries(form.factories)) {
    if (parseCap(text) === undefined) errors[`factory:${id}`] = capError;
  }
  return errors;
}

/**
 * The PATCH body for what the form changed; empty when nothing did. Slots go only when a cap
 * changed, always with the organization's (the contract requires it) and only the factories whose
 * cap changed. Send it only for a form that validates.
 */
export function changedFields(initial: OrgSettings, form: SettingsForm): OrgSettingsPatch {
  const patch: OrgSettingsPatch = {};
  const name = form.name.trim();
  if (name !== initial.name) patch.name = name;
  const mark = form.mark.trim();
  if (mark !== initial.mark) patch.mark = mark;
  const slug = form.slug.trim();
  if (slug !== initial.slug) patch.slug = slug;

  const max = parseCap(form.max) ?? null;
  const factories = initial.slots.factories.flatMap((f) => {
    const cap = parseCap(form.factories[f.factory_id] ?? '');
    return cap === f.max ? [] : [{ factory_id: f.factory_id, max: cap ?? null }];
  });
  if (parseCap(form.max) !== initial.slots.max || factories.length > 0) {
    patch.slots = { max, ...(factories.length > 0 ? { factories } : {}) };
  }
  return patch;
}

/** The sentence under the factory limits; null while a cap is not a number or there are no factories. */
export function slotNote(form: SettingsForm): string | null {
  const max = parseCap(form.max);
  const caps = Object.values(form.factories).map(parseCap);
  if (max === undefined || caps.length === 0 || caps.includes(undefined)) return null;
  if (max === null) {
    return caps.every((c) => c === null)
      ? 'No limits: factories run as many agents as their stations allow.'
      : 'No limit across the organization: each factory stops at its own limit.';
  }
  if (caps.includes(null)) return `A factory without a limit may use all ${max} slots.`;
  const sum = caps.reduce<number>((total, c) => total + (c ?? 0), 0);
  return sum > max
    ? `Limits add up to ${sum} of ${max} slots, so factories wait for each other at peak. That’s allowed.`
    : `Limits add up to ${sum} of ${max} slots.`;
}
