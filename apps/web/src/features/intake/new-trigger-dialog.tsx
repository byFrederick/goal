// New trigger, step 1 of 2: what starts it (intake-new, MASTER.md's two-pane picker). The left pane lists the
// sources; the right pane has the chosen source's events. Continue goes to the trigger page with the choice in
// the address. On phones the panes take turns, with Back from the details.
import { useNavigate } from '@tanstack/react-router';
import { Search } from 'lucide-react';
import { useId, useState } from 'react';
import { useFactoryParams } from '../../shared/factory';
import { Button } from '../../ui/button';
import { cn } from '../../ui/cn';
import { Dialog, DialogClose, DialogContent } from '../../ui/dialog';
import { NotAvailable } from '../../ui/not-available';
import {
  defaultEvent,
  githubEventGroups,
  newTriggerHref,
  type PickerEvent,
  type PickerSource,
  pickerGroups,
  pickerSources,
  type SourceKey,
  sourceByKey,
} from './sources';

export function NewTriggerDialog() {
  const navigate = useNavigate();
  const params = useFactoryParams();
  const [source, setSource] = useState<SourceKey>('github');
  const [event, setEvent] = useState<string>(defaultEvent);
  const [filter, setFilter] = useState('');
  /** Phones show one pane at a time: the sources, then the chosen source's details. */
  const [detailsShown, setDetailsShown] = useState(false);

  const leave = () => navigate({ to: '/o/$org/f/$factory/intake', params });
  const chosen = sourceByKey[source];
  const target = continueTo(chosen, event);
  const href = newTriggerHref(params.org, params.factory, target.route, target.on);

  return (
    <Dialog open onOpenChange={(open) => !open && void leave()}>
      <DialogContent title="New trigger" description="Step 1 of 2: what starts it" className="max-w-4xl">
        <div className="-mx-5 -mb-5 flex flex-col">
          <div className="flex min-h-0 flex-col border-t border-line md:flex-row">
            <fieldset
              className={cn(
                'm-0 flex min-w-0 flex-col gap-1 border-0 bg-panel p-3 md:w-64 md:flex-none md:border-r md:border-line',
                detailsShown && 'max-md:hidden',
              )}
            >
              <legend className="sr-only">Sources</legend>
              <label className="mb-2 flex h-9 items-center gap-2 rounded-control border border-rule bg-card px-3 text-ink-soft">
                <Search aria-hidden="true" className="size-4 flex-none" />
                <span className="sr-only">Search sources</span>
                <input
                  type="search"
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                  placeholder="Search sources"
                  className="min-w-0 flex-1 bg-transparent text-body text-ink outline-none placeholder:text-muted"
                />
              </label>
              {pickerGroups.map((group) => {
                const shown = group.keys
                  .map((key) => pickerSources.find((s) => s.key === key))
                  .filter((s): s is PickerSource => s !== undefined)
                  .filter((s) => s.label.toLowerCase().includes(filter.trim().toLowerCase()));
                if (shown.length === 0) return null;
                return (
                  <div key={group.title} className="flex flex-col gap-1">
                    <h3 className="m-0 px-2 pt-2 pb-1 text-meta font-semibold text-muted">{group.title}</h3>
                    {shown.map((s) => (
                      <SourceButton
                        key={s.key}
                        source={s}
                        pressed={source === s.key}
                        onPick={() => {
                          setSource(s.key);
                          setEvent(defaultEvent);
                          setDetailsShown(true);
                        }}
                      />
                    ))}
                  </div>
                );
              })}
            </fieldset>

            <div className={cn('flex min-w-0 flex-1 flex-col gap-4 p-5', !detailsShown && 'max-md:hidden')}>
              <Button
                variant="ghost"
                size="sm"
                className="-ml-2 self-start md:hidden"
                onClick={() => setDetailsShown(false)}
              >
                Back
              </Button>
              <div className="flex items-center gap-3">
                <SourceTile source={chosen} />
                <h3 className="m-0 text-heading">{chosen.label}</h3>
              </div>
              {chosen.key === 'github' ? (
                <GithubEvents event={event} onChange={setEvent} />
              ) : (
                <p className="m-0 text-body text-ink-soft">{sourceNote(chosen)}</p>
              )}
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-3 border-t border-line p-4">
            <span className="text-secondary text-muted">Next: filters, what it creates and its limits</span>
            <div className="ml-auto flex items-center gap-2">
              <DialogClose asChild>
                <Button variant="secondary">Cancel</Button>
              </DialogClose>
              <Button variant="primary" onClick={() => navigate({ href })}>
                Continue
              </Button>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/** The trigger page's source and event key for a choice (the schedule's key is `schedule`). */
function continueTo(source: PickerSource, event: string): { route: PickerSource['route']; on?: string } {
  if (source.key === 'github') return { route: 'github', on: event };
  if (source.key === 'schedule') return { route: 'schedule', on: 'schedule' };
  return { route: source.route };
}

function sourceNote(source: PickerSource): string {
  if (source.key === 'schedule') return 'Starts on a schedule. The time and its time zone are set on the next step.';
  if (source.key === 'cloudflare')
    return 'Starts when Cloudflare posts an event to a webhook endpoint. The endpoint is chosen on the next step.';
  return 'Starts when a webhook endpoint receives a delivery. The endpoint is chosen on the next step.';
}

function SourceButton({ source, pressed, onPick }: { source: PickerSource; pressed: boolean; onPick: () => void }) {
  const button = (
    <button
      type="button"
      aria-pressed={pressed}
      aria-disabled={source.available ? undefined : 'true'}
      onClick={() => {
        if (source.available) onPick();
      }}
      className={cn(
        'flex h-10 items-center gap-3 rounded-control px-2 text-left text-body font-medium text-ink hover:bg-hover',
        'aria-pressed:border aria-pressed:border-rule aria-pressed:bg-card',
        !source.available && 'text-muted',
      )}
    >
      <SourceTile source={source} />
      {source.label}
    </button>
  );
  return source.available ? button : <NotAvailable label={source.label}>{button}</NotAvailable>;
}

function SourceTile({ source }: { source: PickerSource }) {
  const Icon = source.icon;
  return (
    <span
      aria-hidden="true"
      className={cn('flex size-6 flex-none items-center justify-center rounded-cell text-white', source.tile)}
    >
      <Icon className="size-3.5" />
    </span>
  );
}

/** The GitHub events the picture lists; only the two the schema accepts can be chosen. */
function GithubEvents({ event, onChange }: { event: string; onChange: (event: string) => void }) {
  return (
    <div className="flex flex-col gap-4">
      {githubEventGroups.map((group) => (
        <fieldset key={group.title} className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
          <legend className="mb-2 p-0 text-meta font-semibold text-muted">{group.title}</legend>
          <div className="grid gap-2 sm:grid-cols-2">
            {group.events.map((ev) => (
              <EventOption
                key={ev.label}
                event={ev}
                checked={ev.key !== null && ev.key === event}
                onChange={onChange}
              />
            ))}
          </div>
        </fieldset>
      ))}
    </div>
  );
}

function EventOption({
  event,
  checked,
  onChange,
}: {
  event: PickerEvent;
  checked: boolean;
  onChange: (event: string) => void;
}) {
  const id = useId();
  const disabled = event.key === null;
  const input = (
    <input
      id={id}
      type="radio"
      name="github-event"
      value={event.key ?? event.label}
      checked={checked}
      aria-disabled={disabled ? 'true' : undefined}
      // Controlled: an arrow key that moves onto a disabled event must not choose it.
      onChange={() => {
        if (event.key) onChange(event.key);
      }}
      className="m-0 size-4 flex-none cursor-pointer accent-run"
    />
  );
  return (
    <div
      className={cn(
        'flex min-h-10 items-center gap-3 rounded-cell border border-line px-3 text-secondary text-ink',
        checked && 'border-run bg-run-tint text-run-ink',
        disabled && 'text-muted',
      )}
    >
      {disabled ? <NotAvailable label={event.label}>{input}</NotAvailable> : input}
      <label htmlFor={id} className={cn('flex-1 py-2', disabled ? 'cursor-not-allowed' : 'cursor-pointer')}>
        {event.label}
      </label>
    </div>
  );
}
