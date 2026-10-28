// Define a kind (spec 4 §4.2, canvas "Connection-New"): an organization kind is a kind file
// (contracts/definitions/connection-kind.schema.json), written here as YAML and sent as JSON to PUT
// …/connection-kinds/{metadata.name}. goald checks it (400 connection-kind-invalid names every
// broken rule; 409 connection-kind-name-taken for a platform kind's name); a kind of that name
// the organization already has is replaced, changing its connections from their next run.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, useRef, useState } from 'react';
import { toast } from 'sonner';
import { parse } from 'yaml';
import { putConnectionKind } from '../../api/connections';
import { keys } from '../../api/queries';
import type { ConnectionKindFile } from '../../api/types';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Dialog, DialogClose, DialogContent } from '../../ui/dialog';
import { Field, Textarea } from '../../ui/field';
import { refusalText } from './words';

const example = `apiVersion: goal/v1
kind: ConnectionKind
metadata:
  name: slack
  title: Slack
  description: A bot token for the channels agents read and post in.
spec:
  fields:
    - {name: token, title: Bot token, secret: true, required: true}
  env:
    SLACK_BOT_TOKEN: "{{token}}"
  test: {method: GET, url: "https://slack.com/api/auth.test", headers: {Authorization: "Bearer {{token}}"}}
  docs: Call the Slack Web API with SLACK_BOT_TOKEN.
`;

/** The kind file in the text, and its name; an error message when it is not one. */
export function readKindFile(text: string): { file: ConnectionKindFile; name: string } | { error: string } {
  let doc: unknown;
  try {
    doc = parse(text);
  } catch (err) {
    return { error: `It isn’t YAML: ${err instanceof Error ? err.message.split('\n')[0] : String(err)}` };
  }
  if (typeof doc !== 'object' || doc === null || Array.isArray(doc)) return { error: 'A kind file is a mapping.' };
  const name = (doc as { metadata?: { name?: unknown } }).metadata?.name;
  if (typeof name !== 'string' || name === '') return { error: 'Name the kind in metadata.name.' };
  return { file: doc as ConnectionKindFile, name };
}

export function DefineKindDialog({
  org,
  onClose,
  onDefined,
}: {
  org: string;
  onClose: () => void;
  /** The kind was saved: its name. */
  onDefined: (name: string) => void;
}) {
  const client = useQueryClient();
  const [text, setText] = useState(example);
  const [error, setError] = useState<string | null>(null);
  const ref = useRef<HTMLTextAreaElement>(null);

  const save = useMutation({
    mutationFn: ({ file, name }: { file: ConnectionKindFile; name: string }) => putConnectionKind(org, name, file),
    onSuccess: async (kind) => {
      await client.invalidateQueries({ queryKey: keys.connectionKinds(org) });
      toast.success(`Kind ${kind.title} saved`);
      onDefined(kind.name);
    },
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const read = readKindFile(text);
    if ('error' in read) {
      setError(read.error);
      return;
    }
    setError(null);
    save.mutate(read);
  };

  const problem = error ?? (save.error ? refusalText(save.error) : null);
  return (
    <Dialog open onOpenChange={(open) => (open ? undefined : onClose())}>
      <DialogContent
        title="Define a kind"
        description="A template for connections of a system Goal doesn’t ship: the fields an admin fills in, the variables a run gets, an optional test. Secret fields are write-only."
        initialFocus={ref}
        className="max-w-[640px]"
      >
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          <Field
            label="Kind file (YAML)"
            help="Saving a kind of a name the organization already has replaces it, for every connection of it from their next run."
            error={problem ?? undefined}
          >
            <Textarea
              ref={ref}
              value={text}
              onChange={(e) => setText(e.target.value)}
              rows={14}
              spellCheck={false}
              autoCapitalize="off"
              translate="no"
              className="font-mono text-secondary leading-relaxed"
            />
          </Field>
          {problem ? (
            <div role="alert" className="sr-only">
              <Callout tone="rework">{problem}</Callout>
            </div>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary" size="lg" className="md:h-10 md:text-sm">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" variant="primary" size="lg" className="md:h-10 md:text-sm" disabled={save.isPending}>
              {save.isPending ? 'Saving…' : 'Save kind'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
