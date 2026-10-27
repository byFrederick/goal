// An agent's skills (D §10.7): a chip per skill with "Remove x", and an ARIA 1.2 combobox, "Add
// a skill from the catalog…", whose options are grouped: the factory's skills the agent lacks,
// then the skills.sh catalog (searched 250 ms after typing stops). Choosing a catalog skill shows
// its SKILL.md as plain text before Add vendors it into the draft in one save (files under
// skills/<skill>/, the factory skill with its origin, the name on the agent).
import { useQuery } from '@tanstack/react-query';
import { Search, TriangleAlert, X } from 'lucide-react';
import { type KeyboardEvent, useEffect, useId, useRef, useState } from 'react';
import { toast } from 'sonner';
import type { SkillKey } from '../../../api/builder';
import { ApiError } from '../../../api/client';
import { skillFilesQuery, skillsQuery } from '../../../api/queries';
import type { SkillListing } from '../../../api/types';
import type { ModelAgent } from '../../../shared/definition/model';
import { Button } from '../../../ui/button';
import { cn } from '../../../ui/cn';
import { Dialog, DialogClose, DialogContent } from '../../../ui/dialog';
import { addSkillFromCatalog, addSkillToAgent, removeSkillFromAgent, SkillExistsError } from '../draft/ops';
import { useDraftSession, useSection, useViewOnly } from '../draft/use-draft';
import { useProblems } from '../frame/use-editor';

/** The value, once it has stopped changing for `ms`. */
function useDebounced<T>(value: T, ms: number): T {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setSettled(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return settled;
}

type Option = { kind: 'local'; name: string } | { kind: 'catalog'; listing: SkillListing };

const optionKey = (o: Option) => (o.kind === 'local' ? `local:${o.name}` : `catalog:${o.listing.key}`);

const installs = new Intl.NumberFormat('en', { notation: 'compact' });

export function SkillsEditor({ agent, index, openPicker }: { agent: ModelAgent; index: number; openPicker: boolean }) {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const agents = useSection('agents');
  const headingId = useId();
  const listId = useId();
  const input = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState('');
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [adding, setAdding] = useState<SkillListing | null>(null);
  const problems = useProblems(`/agents/agents/${index}/skills`);
  const q = useDebounced(query.trim(), 250);
  const catalog = useQuery({ ...skillsQuery(session.org, q), enabled: open });

  const mine = agent.skills ?? [];
  const needle = query.trim().toLowerCase();
  const local: Option[] = (agents.skills ?? [])
    .filter((s) => !mine.includes(s.name) && s.name.toLowerCase().includes(needle))
    .map((s) => ({ kind: 'local', name: s.name }));
  const remote: Option[] = (catalog.data?.skills ?? []).map((listing) => ({ kind: 'catalog', listing }));
  const options = [...local, ...remote];
  const activeIndex = Math.min(active, options.length - 1);

  useEffect(() => {
    if (!openPicker) return;
    input.current?.focus();
    setOpen(true);
  }, [openPicker]);

  const choose = (o: Option) => {
    if (o.kind === 'local') {
      session.edit(addSkillToAgent(agents, agent.name, o.name));
      setQuery('');
      return;
    }
    setOpen(false);
    setAdding(o.listing);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      setOpen(true);
      if (options.length === 0) return;
      const step = e.key === 'ArrowDown' ? 1 : -1;
      setActive((activeIndex + step + options.length) % options.length);
    } else if (e.key === 'Enter' && open) {
      const o = options[activeIndex];
      if (!o) return;
      e.preventDefault();
      choose(o);
    } else if (e.key === 'Escape' && open) {
      e.preventDefault();
      setOpen(false);
    }
  };

  const optionId = (i: number) => `${listId}-${i}`;
  const showList = open && (options.length > 0 || !catalog.isPending);
  const groups = [
    { label: 'In this factory', items: local, offset: 0 },
    { label: 'skills.sh', items: remote, offset: local.length },
  ].filter((g) => g.items.length > 0);

  return (
    <div className="flex min-w-0 flex-col gap-2.5">
      <h4 id={headingId} className="m-0 text-[15px] font-medium">
        Skills
      </h4>
      {mine.length > 0 ? (
        <ul aria-labelledby={headingId} className="m-0 flex list-none flex-wrap gap-1.5 p-0">
          {mine.map((name, j) => {
            const problem = problems.find((p) => p.pointer === `/agents/agents/${index}/skills/${j}`);
            return (
              <li
                key={name}
                className={cn(
                  'inline-flex h-11 items-center gap-1 rounded-chip border bg-card pr-1 pl-2.5 text-[13px] md:h-8',
                  problem ? 'border-rework text-rework-ink' : 'border-line',
                )}
              >
                {problem ? <TriangleAlert size={13} strokeWidth={1.75} aria-hidden="true" /> : null}
                <span translate="no" className="font-mono">
                  {name}
                </span>
                {problem ? <span className="sr-only">, {problem.message}</span> : null}
                {viewOnly ? (
                  <span className="w-1.5" />
                ) : (
                  <button
                    type="button"
                    aria-label={`Remove ${name}`}
                    onClick={() => session.edit(removeSkillFromAgent(agents, agent.name, name))}
                    className="inline-flex size-9 items-center justify-center rounded-chip text-muted transition-colors duration-200 hover:bg-shell hover:text-ink md:size-6"
                  >
                    <X size={14} strokeWidth={1.5} aria-hidden="true" />
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      ) : (
        <p className="m-0 text-sm text-muted">No skills yet.</p>
      )}
      {problems.length > 0 ? (
        <ul className="m-0 flex list-none flex-col gap-0.5 p-0 text-[13px] leading-snug text-rework-ink">
          {problems.map((p) => (
            <li key={`${p.code}:${p.pointer}`}>{p.message}</li>
          ))}
        </ul>
      ) : null}

      {viewOnly ? null : (
        <div className="relative">
          <div className="flex h-11 items-center gap-2 rounded-control border border-rule bg-card px-3 text-muted transition-colors duration-200 hover:border-muted has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-offset-2 has-[input:focus-visible]:outline-run md:h-10">
            <Search size={16} strokeWidth={1.5} aria-hidden="true" />
            <input
              ref={input}
              role="combobox"
              aria-label="Add a skill from the catalog"
              aria-expanded={showList}
              aria-controls={listId}
              aria-autocomplete="list"
              aria-activedescendant={showList && options[activeIndex] ? optionId(activeIndex) : undefined}
              placeholder="Add a skill from the catalog…"
              autoComplete="off"
              spellCheck={false}
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setActive(0);
                setOpen(true);
              }}
              onFocus={() => setOpen(true)}
              onClick={() => setOpen(true)}
              onBlur={() => setOpen(false)}
              onKeyDown={onKeyDown}
              className="h-full min-w-0 flex-1 border-0 bg-transparent text-base text-ink placeholder:text-muted focus-visible:outline-none md:text-[15px]"
            />
          </div>
          <div
            id={listId}
            role="listbox"
            aria-label="Skills"
            hidden={!showList}
            className="absolute top-full right-0 left-0 z-20 mt-1 max-h-80 overflow-y-auto rounded-card border border-rule bg-card p-1.5 shadow-pop"
          >
            {groups.map((g, gi) => (
              // biome-ignore lint/a11y/useSemanticElements: a group of a combobox's listbox (ARIA 1.2), not a form fieldset.
              <div key={g.label} role="group" aria-labelledby={`${listId}-g${gi}`}>
                <div
                  id={`${listId}-g${gi}`}
                  role="presentation"
                  className="px-2.5 pt-2 pb-1 text-xs font-medium text-muted"
                >
                  {g.label}
                </div>
                {g.items.map((o, j) => {
                  const i = g.offset + j;
                  return (
                    // biome-ignore lint/a11y/useKeyWithClickEvents: the combobox's input handles the keys (aria-activedescendant).
                    // biome-ignore lint/a11y/useFocusableInteractive: options are reached through aria-activedescendant; focus stays in the input.
                    <div
                      key={optionKey(o)}
                      id={optionId(i)}
                      role="option"
                      aria-selected={i === activeIndex}
                      onMouseDown={(e) => e.preventDefault()}
                      onMouseMove={() => setActive(i)}
                      onClick={() => choose(o)}
                      className={cn(
                        'flex min-h-11 cursor-pointer flex-col justify-center rounded-control px-2.5 py-1.5',
                        i === activeIndex && 'bg-shell',
                      )}
                    >
                      {o.kind === 'local' ? (
                        <span translate="no" className="font-mono text-[13px]">
                          {o.name}
                        </span>
                      ) : (
                        <>
                          <span className="flex flex-wrap items-baseline gap-x-2 text-sm">
                            <span translate="no" className="font-mono text-[13px] text-ink">
                              {o.listing.skill}
                            </span>
                            <span className="text-xs text-muted">
                              {o.listing.owner}/{o.listing.repo} · {installs.format(o.listing.installs)} installs
                            </span>
                          </span>
                          <span className="line-clamp-2 text-[13px] text-muted">{o.listing.description}</span>
                        </>
                      )}
                    </div>
                  );
                })}
              </div>
            ))}
            {options.length === 0 ? (
              <p className="m-0 px-2.5 py-2 text-sm text-muted">
                {catalog.isError
                  ? 'The skills catalog can’t be reached. Try again in a moment.'
                  : query.trim()
                    ? `No skills match ${query.trim()}`
                    : 'No skills in the catalog yet. An operator adds them with goal catalog skills sync.'}
              </p>
            ) : null}
          </div>
        </div>
      )}

      <AddSkillDialog
        agent={agent}
        listing={adding}
        onClose={(added) => {
          setAdding(null);
          if (added) setQuery('');
          input.current?.focus();
        }}
      />
    </div>
  );
}

/** A catalog skill's SKILL.md as plain text, the warning, and Add. */
function AddSkillDialog({
  agent,
  listing,
  onClose,
}: {
  agent: ModelAgent;
  listing: SkillListing | null;
  onClose: (added: boolean) => void;
}) {
  const session = useDraftSession();
  const agents = useSection('agents');
  const [error, setError] = useState<string>();
  const files = useQuery({
    ...skillFilesQuery(session.org, (listing?.key ?? 'x/x/x') as SkillKey),
    enabled: listing !== null,
  });
  const skillMd = files.data?.files['SKILL.md'];
  const text = skillMd && 'text' in skillMd ? skillMd.text : undefined;
  const count = files.data ? Object.keys(files.data.files).length : listing?.files;

  const add = () => {
    if (!files.data) return;
    try {
      session.edit(addSkillFromCatalog(agents, agent.name, files.data));
    } catch (err) {
      if (err instanceof SkillExistsError) {
        setError(err.message);
        return;
      }
      throw err;
    }
    toast.success(`${files.data.skill.skill} added to ${agent.name}`);
    onClose(true);
  };

  return (
    <Dialog
      open={listing !== null}
      onOpenChange={(open) => {
        if (open) return;
        setError(undefined);
        onClose(false);
      }}
    >
      {listing ? (
        <DialogContent
          title={`Add ${listing.skill}`}
          description={`${listing.owner}/${listing.repo}, pinned at ${listing.commit.slice(0, 7)}${count !== undefined ? `, ${count === 1 ? '1 file' : `${count} files`}` : ''}`}
          className="max-w-[640px]"
          onCloseAutoFocus={(e) => e.preventDefault()}
        >
          <p className="m-0 flex gap-2 rounded-control border border-amber-tint bg-amber-tint px-3 py-2.5 text-sm text-ink">
            <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" className="mt-0.5 shrink-0 text-amber-ink" />
            Skills run inside this agent’s container with its tools. Review it before you publish.
          </p>
          {files.isPending ? (
            <p className="m-0 text-sm text-muted">Loading SKILL.md…</p>
          ) : files.isError ? (
            <p role="alert" className="m-0 text-sm text-rework-ink">
              {files.error instanceof ApiError && files.error.code === 'skill-not-found'
                ? 'This skill is no longer in the catalog.'
                : 'The skills catalog can’t be reached. Try again in a moment.'}
            </p>
          ) : (
            <section
              aria-label="SKILL.md"
              // biome-ignore lint/a11y/noNoninteractiveTabindex: a scrolling region must be reachable by keyboard.
              tabIndex={0}
              className="max-h-[45dvh] overflow-auto rounded-control border border-line bg-card p-3"
            >
              <pre className="m-0 font-mono text-[13px] leading-[1.6] whitespace-pre-wrap text-ink [overflow-wrap:anywhere]">
                {text ?? 'This skill has no SKILL.md.'}
              </pre>
            </section>
          )}
          {error ? (
            <p role="alert" className="m-0 text-[13px] text-rework-ink">
              {error}
            </p>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary" size="lg" className="md:h-10 md:text-sm">
                Cancel
              </Button>
            </DialogClose>
            <Button
              variant="primary"
              size="lg"
              className="md:h-10 md:text-sm"
              disabled={!files.data || error !== undefined}
              onClick={add}
            >
              Add skill
            </Button>
          </div>
        </DialogContent>
      ) : null}
    </Dialog>
  );
}
