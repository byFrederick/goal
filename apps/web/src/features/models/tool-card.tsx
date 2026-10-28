// "Tool policy" (canvas "Models and policies"): what agents may do without a person, four defaults the
// factory definitions may choose less than (ADR 0024, decision 2). All four are enforced at run start
// (the tool-policy change), so every row is a live select.
import { useId } from 'react';
import type { ToolPolicy } from '../../api/types';
import { SectionCard } from '../../ui/card';
import { NativeSelect } from '../../ui/field';
import { type PolicyForm, toolOptions } from './form';

const rows: { key: keyof ToolPolicy; title: string; help: string }[] = [
  {
    key: 'write_outside_branch',
    title: "Write outside the item's branch",
    help: 'Comments, issues, tickets, messages',
  },
  { key: 'merge_pull_request', title: 'Merge a pull request', help: 'Only a review station with merge on approval' },
  { key: 'read_web', title: 'Read the web and docs', help: 'Search, fetch, context7' },
  { key: 'use_credentials', title: 'Use credentials', help: 'Only the ones attached to the agent' },
];

export function ToolCard({ form, onChange }: { form: PolicyForm; onChange: (next: PolicyForm) => void }) {
  return (
    <SectionCard
      title="Tool policy"
      aside={<span className="text-secondary text-muted">What agents may do without a person</span>}
    >
      <div className="flex flex-col">
        {rows.map((row) => (
          <ToolRow
            key={row.key}
            title={row.title}
            help={row.help}
            value={form.tools[row.key]}
            options={toolOptions[row.key]}
            onChange={(value) => onChange({ ...form, tools: { ...form.tools, [row.key]: value } })}
          />
        ))}
      </div>
    </SectionCard>
  );
}

/** One tool setting: its name and what it covers at the left, the select at the right. */
function ToolRow({
  title,
  help,
  value,
  options,
  onChange,
}: {
  title: string;
  help: string;
  value: string;
  options: readonly { value: string; label: string }[];
  onChange: (value: ToolPolicy[keyof ToolPolicy]) => void;
}) {
  const id = useId();
  return (
    <div className="flex flex-col gap-2 border-t border-line py-3 first:border-t-0 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
      <div className="flex min-w-0 flex-col gap-1">
        <label htmlFor={id} className="font-medium text-ink">
          {title}
        </label>
        <span id={`${id}-help`} className="text-secondary text-muted">
          {help}
        </span>
      </div>
      <NativeSelect
        id={id}
        aria-describedby={`${id}-help`}
        value={value}
        onChange={(e) => onChange(e.target.value as ToolPolicy[keyof ToolPolicy])}
        className="h-11 w-auto shrink-0 md:h-8 md:text-secondary"
      >
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </NativeSelect>
    </div>
  );
}
