// The Models and policies form: the policy as goald answered it, edited in place. These are the pure
// rules the page uses: how a model switch changes the default, how a denied command is written and
// shown, how the box time limit reads in hours without losing its minutes, and when the form differs
// from what was loaded.
import type { OrgPolicyInput, ToolPolicy } from '../../api/types';

export type PolicyForm = OrgPolicyInput;

/** The form a loaded policy starts from. */
export const formOf = (policy: OrgPolicyInput): PolicyForm => ({
  ...policy,
  allowed_models: [...policy.allowed_models],
  denied_commands: [...policy.denied_commands],
  tools: { ...policy.tools },
});

/** Whether the form asks for something other than the loaded policy. */
export const changed = (loaded: PolicyForm, form: PolicyForm): boolean =>
  JSON.stringify(loaded) !== JSON.stringify(form);

/**
 * Turns a model on or off. Turning off the default moves the default to the first model still on,
 * so the policy stays valid; the last model on cannot go off, since a policy needs one.
 */
export function toggleModel(form: PolicyForm, id: string, models: readonly string[]): PolicyForm {
  const on = form.allowed_models.includes(id);
  if (on) {
    const rest = form.allowed_models.filter((m) => m !== id);
    if (rest.length === 0) return form;
    const next = models.find((m) => rest.includes(m)) ?? rest[0] ?? form.default_model;
    return { ...form, allowed_models: rest, default_model: form.default_model === id ? next : form.default_model };
  }
  return { ...form, allowed_models: models.filter((m) => m === id || form.allowed_models.includes(m)) };
}

/** The Bash pattern a denied command is stored as: `terraform apply` becomes `Bash(terraform apply:*)`. */
export const patternOf = (command: string): string => `Bash(${command.trim()}:*)`;

/** The command a pattern shows as: the text inside `Bash(…:*)`, or the pattern itself when it has another form. */
export function commandOf(pattern: string): string {
  const match = /^Bash\((.*?)(?::\*)?\)$/.exec(pattern);
  return match?.[1] ?? pattern;
}

/** The box time limit in hours, as a short decimal: 180 minutes is "3", 90 is "1.5". */
export const hoursOf = (minutes: number): string => String(Number((minutes / 60).toFixed(2)));

/** The minutes an hours entry means, or null when it is not a number above zero that is at least a minute. */
export function minutesOf(hours: string): number | null {
  const value = Number(hours.trim().replace(',', '.'));
  if (hours.trim() === '' || !Number.isFinite(value) || value <= 0) return null;
  const minutes = Math.round(value * 60);
  return minutes >= 1 ? minutes : null;
}

/** A box's memory in whole gigabytes, from the bytes goald answers. */
export const gigabytes = (bytes: number): number => Math.round(bytes / 1024 ** 3);

/** Each tool policy's options, as the select shows them, in the order the API lists them. */
export const toolOptions: { [K in keyof ToolPolicy]: readonly { value: ToolPolicy[K]; label: string }[] } = {
  write_outside_branch: [
    { value: 'off', label: 'Off' },
    { value: 'ask', label: 'Ask a person' },
    { value: 'allowed', label: 'Allowed' },
  ],
  merge_pull_request: [
    { value: 'off', label: 'Off' },
    { value: 'review_approval', label: 'Review approval' },
  ],
  read_web: [
    { value: 'off', label: 'Off' },
    { value: 'ask', label: 'Ask a person' },
    { value: 'allowed', label: 'Allowed' },
  ],
  use_credentials: [
    { value: 'off', label: 'Off' },
    { value: 'attached_only', label: 'Attached only' },
  ],
};
