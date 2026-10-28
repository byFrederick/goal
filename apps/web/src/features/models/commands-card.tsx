// "Commands no agent may run" (canvas "Models and policies"): the denied commands, each a chip with a
// remove button, and "+ Add a command". A denied command fails the tool call and shows on the item's
// timeline. Stored as Claude Code Bash patterns (ADR 0024); shown as the command the person typed.
import { X } from 'lucide-react';
import { type FormEvent, useState } from 'react';
import { Button } from '../../ui/button';
import { SectionCard } from '../../ui/card';
import { Field, Input } from '../../ui/field';
import { useReadOnly } from '../../ui/read-only';
import { commandOf, type PolicyForm, patternOf } from './form';

export function CommandsCard({ form, onChange }: { form: PolicyForm; onChange: (next: PolicyForm) => void }) {
  const [adding, setAdding] = useState(false);
  const [text, setText] = useState('');
  const shown = useReadOnly();

  const add = (e: FormEvent) => {
    e.preventDefault();
    const command = text.trim();
    if (command === '') return;
    const pattern = patternOf(command);
    if (!form.denied_commands.includes(pattern)) {
      onChange({ ...form, denied_commands: [...form.denied_commands, pattern] });
    }
    setText('');
    setAdding(false);
  };

  return (
    <SectionCard
      title="Commands no agent may run"
      aside={<span className="text-secondary text-muted">In every factory</span>}
    >
      <ul className="m-0 flex list-none flex-wrap gap-2 p-0">
        {form.denied_commands.map((pattern) => {
          const command = commandOf(pattern);
          return (
            <li
              key={pattern}
              className="inline-flex h-7 items-center gap-1 rounded-control bg-rework-tint pr-1 pl-2 font-mono text-secondary text-rework-ink"
            >
              {command}
              {shown ? null : (
                <button
                  type="button"
                  aria-label={`Remove ${command}`}
                  onClick={() =>
                    onChange({ ...form, denied_commands: form.denied_commands.filter((p) => p !== pattern) })
                  }
                  className="grid size-5 place-items-center rounded-chip hover:bg-card"
                >
                  <X size={12} strokeWidth={1.6} aria-hidden="true" />
                </button>
              )}
            </li>
          );
        })}
      </ul>
      {adding && !shown ? (
        <form onSubmit={add} className="flex flex-col gap-2 sm:flex-row sm:items-end">
          <Field
            label="Command"
            className="min-w-0 flex-1"
            help="The command as an agent types it, such as terraform apply"
          >
            <Input value={text} onChange={(e) => setText(e.target.value)} className="font-mono" />
          </Field>
          <div className="flex gap-2">
            <Button type="submit" variant="primary" size="sm" disabled={text.trim() === ''}>
              Add
            </Button>
            <Button type="button" variant="white" size="sm" onClick={() => setAdding(false)}>
              Cancel
            </Button>
          </div>
        </form>
      ) : shown ? null : (
        <div>
          <Button variant="white" size="sm" onClick={() => setAdding(true)}>
            + Add a command
          </Button>
        </div>
      )}
      <p className="m-0 text-meta text-muted">A denied command fails the tool call and shows on the item's timeline.</p>
    </SectionCard>
  );
}
