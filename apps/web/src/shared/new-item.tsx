import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { Plus } from 'lucide-react';
import { type FormEvent, useId, useState } from 'react';
import { toast } from 'sonner';
import { submitItem } from '../api/client';
import type { Factory } from '../api/types';
import { Button } from '../ui/button';
import { Dialog, DialogClose, DialogContent, DialogTrigger } from '../ui/dialog';
import { refParam } from './factory';
import { humanize } from './format';
import { factoryWorkInvalidations } from './invalidation';
import { useOrg } from './org';
import { holds } from './permissions';

const field =
  'w-full rounded-control border border-rule bg-card px-3 text-base text-ink placeholder:text-muted aria-[invalid=true]:border-rework';

interface NewItemProps {
  factory: Factory;
  compact?: boolean;
}

/**
 * "New item": submits work to the factory's first station. Not offered without item:create
 * (viewers): they have nothing to submit, and goald would refuse it.
 */
export function NewItemButton(props: NewItemProps) {
  return holds(props.factory, 'item:create') ? <NewItem {...props} /> : null;
}

function NewItem({ factory, compact }: NewItemProps) {
  const id = useId();
  const client = useQueryClient();
  const org = useOrg();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState('');
  const [type, setType] = useState(factory.work_types[0] ?? '');
  const [body, setBody] = useState('');
  const [touched, setTouched] = useState(false);

  const submit = useMutation({
    mutationFn: () =>
      submitItem(org.id, factory.id, {
        title: title.trim(),
        ...(type ? { type } : {}),
        ...(body.trim() ? { body: body.trim() } : {}),
      }),
    onSuccess: (item) => {
      setOpen(false);
      setTitle('');
      setBody('');
      setTouched(false);
      toast.success(`Created ${item.ref}. It waits at ${humanize(item.station)}.`);
      for (const queryKey of factoryWorkInvalidations(org.id, factory.id)) client.invalidateQueries({ queryKey });
      navigate({
        to: '/o/$org/f/$factory/items/$ref',
        params: { org: org.slug, factory: factory.name, ref: refParam(item.ref) },
      });
    },
    onError: (err) => toast.error('Could not create the item', { description: err.message }),
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (title.trim()) submit.mutate();
  };

  const missing = touched && !title.trim();

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        {compact ? (
          <Button variant="primary" size="icon" className="size-11" aria-label="New item">
            <Plus size={18} strokeWidth={1.75} aria-hidden="true" />
          </Button>
        ) : (
          <Button variant="primary">
            <Plus size={16} strokeWidth={1.75} aria-hidden="true" />
            New item
          </Button>
        )}
      </DialogTrigger>
      <DialogContent
        title="New item"
        description={`It enters ${humanize(factory.stations[0]?.id ?? 'the line')} and waits to be pulled.`}
      >
        <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
          <div className="flex flex-col gap-1.5">
            <label htmlFor={`${id}-title`} className="text-sm text-muted">
              Title
            </label>
            <input
              id={`${id}-title`}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              maxLength={200}
              required
              aria-invalid={missing}
              aria-describedby={missing ? `${id}-error` : undefined}
              className={`${field} h-11`}
            />
            {missing ? (
              <p id={`${id}-error`} className="m-0 text-sm text-rework">
                Give the item a title.
              </p>
            ) : null}
          </div>
          {factory.work_types.length > 0 ? (
            <div className="flex flex-col gap-1.5">
              <label htmlFor={`${id}-type`} className="text-sm text-muted">
                Work type
              </label>
              <select
                id={`${id}-type`}
                value={type}
                onChange={(e) => setType(e.target.value)}
                className={`${field} h-11`}
              >
                {factory.work_types.map((t) => (
                  <option key={t} value={t}>
                    {humanize(t)}
                  </option>
                ))}
              </select>
            </div>
          ) : null}
          <div className="flex flex-col gap-1.5">
            <label htmlFor={`${id}-body`} className="text-sm text-muted">
              Details <span className="text-muted">(optional)</span>
            </label>
            <textarea
              id={`${id}-body`}
              value={body}
              onChange={(e) => setBody(e.target.value)}
              rows={4}
              maxLength={20000}
              className={`${field} min-h-24 resize-y py-2.5`}
            />
          </div>
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={submit.isPending}>
              {submit.isPending ? 'Creating…' : 'Create item'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
