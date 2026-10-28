import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useMatch, useNavigate } from '@tanstack/react-router';
import { Plus } from 'lucide-react';
import { type FormEvent, type ReactNode, useEffect, useId, useState } from 'react';
import { toast } from 'sonner';
import { submitItem } from '../api/client';
import { factoriesQuery } from '../api/queries';
import type { Factory } from '../api/types';
import { Button } from '../ui/button';
import { Dialog, DialogClose, DialogContent, DialogTrigger } from '../ui/dialog';
import { findFactory, refParam } from './factory';
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
  const [open, setOpen] = useState(false);
  return (
    <Composer
      open={open}
      onOpenChange={setOpen}
      factory={factory}
      trigger={
        compact ? (
          <Button variant="primary" size="icon" className="size-11" aria-label="New item">
            <Plus size={18} strokeWidth={1.75} aria-hidden="true" />
          </Button>
        ) : (
          <Button variant="primary">
            <Plus size={16} strokeWidth={1.75} aria-hidden="true" />
            New item
          </Button>
        )
      }
    />
  );
}

/**
 * The sidebar's "New item" (MASTER.md "The shell"): the same composer, with a factory picker. It starts
 * on the open factory and is empty elsewhere. C opens it too, unless focus is in a field or a dialog is
 * open. Hidden once the list of factories loads with none the person may add items to.
 * With `button` false it only listens for C and renders the composer: the collapsed rail has no
 * New item button yet, but C must still open the composer there and on phones.
 */
export function NewItemFromSidebar({
  button = true,
  openWith,
  onOpened,
}: {
  button?: boolean;
  /** A request to open the composer with a title, from the sidebar's search; `onOpened` clears it. */
  openWith?: { title: string } | null;
  onOpened?: () => void;
}) {
  const org = useOrg();
  const factories = useQuery(factoriesQuery(org.id));
  const current = useMatch({ from: '/o/$org/f/$factory', shouldThrow: false });
  const [open, setOpen] = useState(false);
  const [picked, setPicked] = useState('');

  const status = factories.isError ? 'error' : factories.isPending ? 'loading' : 'ready';
  const choices = (factories.data ?? [])
    .filter((f) => !f.archived_at && holds(f, 'item:create'))
    .toSorted((a, b) => a.name.localeCompare(b.name));
  const startAt = current ? (findFactory(choices, current.params.factory)?.id ?? '') : '';
  const shown = status !== 'ready' || choices.length > 0;

  useEffect(() => {
    if (!openWith) return;
    setPicked(startAt);
    setOpen(true);
    onOpened?.();
  }, [openWith, startAt, onOpened]);

  useEffect(() => {
    if (!shown) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || !isNewItemKey(e) || isTyping(e.target)) return;
      if (document.querySelector('[role="dialog"]')) return;
      e.preventDefault();
      setPicked(startAt);
      setOpen(true);
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [shown, startAt]);

  // An open dialog stays mounted, so a list that empties while it is open shows the picker's empty state.
  if (!shown && !open) return null;

  return (
    <Composer
      open={open}
      onOpenChange={(next) => {
        if (next) setPicked(startAt);
        setOpen(next);
      }}
      initialTitle={openWith?.title}
      picker={{
        status,
        factories: choices,
        value: picked,
        onChange: setPicked,
        onRetry: () => void factories.refetch(),
      }}
      trigger={
        shown && button ? (
          <button
            type="button"
            aria-keyshortcuts="C"
            className="flex h-9 w-full items-center gap-2 rounded-control bg-run pr-2 pl-3 font-control text-white"
          >
            <Plus size={16} strokeWidth={1.5} aria-hidden="true" />
            <span className="flex-1 text-left text-body">New item</span>
            <kbd className="grid size-5 place-items-center rounded-chip border border-white/40 text-meta font-semibold">
              C
            </kbd>
          </button>
        ) : undefined
      }
    />
  );
}

/** The shortcut C with no modifiers and no key repeat (the same rules as isMoveShortcut). */
export function isNewItemKey(e: KeyboardEvent): boolean {
  return e.key.toLowerCase() === 'c' && !e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey && !e.repeat;
}

const typingTargets = 'input, textarea, select, [contenteditable]:not([contenteditable="false"]), .cm-editor';

function isTyping(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest(typingTargets) !== null;
}

/** The sidebar's factory picker: the factories the person may add items to, and where they stand. */
interface Picker {
  status: 'loading' | 'error' | 'ready';
  factories: Factory[];
  value: string;
  onChange: (id: string) => void;
  onRetry: () => void;
}

interface ComposerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The factory the item goes to, when the page fixes it. */
  factory?: Factory;
  /** The factory picker of the sidebar's New item, in place of a fixed factory. */
  picker?: Picker;
  /** Opens the dialog; without one the dialog opens only by the shortcut, or stays open to show its state. */
  trigger?: ReactNode;
  /** The title the dialog opens with, from the sidebar's search; read when the dialog mounts. */
  initialTitle?: string | undefined;
}

/** The "New item" dialog: the title, the factory (with a picker), the work type and the details. */
function Composer({ open, onOpenChange, factory, picker, trigger, initialTitle }: ComposerProps) {
  const id = useId();
  const client = useQueryClient();
  const org = useOrg();
  const navigate = useNavigate();
  // The sidebar's search unmounts the sidebar's New item while it is open, so this mounts with the title it opens with.
  const [title, setTitle] = useState(initialTitle ?? '');
  const [type, setType] = useState('');
  const [body, setBody] = useState('');
  const [touched, setTouched] = useState(false);

  const target = factory ?? picker?.factories.find((f) => f.id === picker.value);
  const workTypes = target?.work_types ?? [];
  const workType = workTypes.includes(type) ? type : (workTypes[0] ?? '');

  const submit = useMutation({
    mutationFn: (to: Factory) =>
      submitItem(org.id, to.id, {
        title: title.trim(),
        ...(workType ? { type: workType } : {}),
        ...(body.trim() ? { body: body.trim() } : {}),
      }),
    onSuccess: (item, to) => {
      onOpenChange(false);
      setTitle('');
      setBody('');
      setTouched(false);
      toast.success(`Created ${item.ref}. It waits at ${humanize(item.station)}.`);
      for (const queryKey of factoryWorkInvalidations(org.id, to.id)) client.invalidateQueries({ queryKey });
      navigate({
        to: '/o/$org/f/$factory/items/$ref',
        params: { org: org.slug, factory: to.name, ref: refParam(item.ref) },
      });
    },
    onError: (err) => toast.error('Could not create the item', { description: err.message }),
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (title.trim() && target) submit.mutate(target);
  };

  const missing = touched && !title.trim();
  const missingFactory = touched && !target;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {trigger ? <DialogTrigger asChild>{trigger}</DialogTrigger> : null}
      <DialogContent
        title="New item"
        description={
          target
            ? `It enters ${humanize(target.stations[0]?.id ?? 'the line')} and waits to be pulled.`
            : 'Choose a factory. The item enters its first station and waits to be pulled.'
        }
      >
        <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
          {picker ? <FactoryField id={id} picker={picker} missing={missingFactory} /> : null}
          <div className="flex flex-col gap-1">
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
          {workTypes.length > 0 ? (
            <div className="flex flex-col gap-1">
              <label htmlFor={`${id}-type`} className="text-sm text-muted">
                Work type
              </label>
              <select
                id={`${id}-type`}
                value={workType}
                onChange={(e) => setType(e.target.value)}
                className={`${field} h-11`}
              >
                {workTypes.map((t) => (
                  <option key={t} value={t}>
                    {humanize(t)}
                  </option>
                ))}
              </select>
            </div>
          ) : null}
          <div className="flex flex-col gap-1">
            <label htmlFor={`${id}-body`} className="text-sm text-muted">
              Details <span className="text-muted">(optional)</span>
            </label>
            <textarea
              id={`${id}-body`}
              value={body}
              onChange={(e) => setBody(e.target.value)}
              rows={4}
              maxLength={20000}
              className={`${field} min-h-24 resize-y py-2`}
            />
          </div>
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={submit.isPending || picker?.status === 'loading'}>
              {submit.isPending ? 'Creating…' : 'Create item'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** The factory picker's four states: loading (busy, no choice yet), error (with Retry), empty, ready. */
function FactoryField({ id, picker, missing }: { id: string; picker: Picker; missing: boolean }) {
  if (picker.status === 'error') {
    return (
      <div className="flex flex-col gap-1">
        <p className="m-0 text-sm text-muted">Factory</p>
        <p role="alert" className="m-0 text-sm text-rework">
          Could not load your factories.
        </p>
        <Button variant="secondary" className="self-start" onClick={picker.onRetry}>
          Retry
        </Button>
      </div>
    );
  }
  if (picker.status === 'ready' && picker.factories.length === 0) {
    return (
      <div className="flex flex-col gap-1">
        <p className="m-0 text-sm text-muted">Factory</p>
        <p className="m-0 text-sm text-ink">No factory takes new items from you.</p>
      </div>
    );
  }
  const loading = picker.status === 'loading';
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={`${id}-factory`} className="text-sm text-muted">
        Factory
      </label>
      <select
        id={`${id}-factory`}
        value={loading ? '' : picker.value}
        onChange={(e) => picker.onChange(e.target.value)}
        disabled={loading}
        aria-busy={loading}
        aria-invalid={missing}
        aria-describedby={missing ? `${id}-factory-error` : undefined}
        className={`${field} h-11`}
      >
        {loading ? (
          <option value="">Loading…</option>
        ) : (
          <>
            <option value="">Choose a factory</option>
            {picker.factories.map((f) => (
              <option key={f.id} value={f.id}>
                {f.name}
              </option>
            ))}
          </>
        )}
      </select>
      {missing ? (
        <p id={`${id}-factory-error`} className="m-0 text-sm text-rework">
          Choose a factory for the item.
        </p>
      ) : null}
    </div>
  );
}
