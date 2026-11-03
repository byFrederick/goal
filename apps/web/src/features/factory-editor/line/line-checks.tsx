// Checks on this line (D §10.6): B's checks of the line section (passed, warning, failed) and its
// line problems, then hints that are never rules, from data already loaded: who needs pull
// request access for a merge step (wizard), a note when nothing merges, and the open items a
// removed station holds now.
import { Check, CircleAlert, Info, TriangleAlert } from 'lucide-react';
import { useId } from 'react';
import type { CheckResult } from '../../../api/types';
import { sectionHeadingClass } from '../../../ui/page-header';
import { useValidation } from '../frame/use-editor';
import { noMergeHint, stationLabel } from './labels';
import { useLineEditor } from './line-editor';

interface Row {
  key: string;
  tone: 'ok' | 'info' | 'rework';
  text: string;
}

const icons = {
  ok: { Icon: Check, label: 'Passed', className: 'text-ok-ink' },
  info: { Icon: Info, label: 'Note', className: 'text-ink' },
  rework: { Icon: TriangleAlert, label: 'Problem', className: 'text-rework-ink' },
} as const;

const toneOf = (c: CheckResult): Row['tone'] =>
  c.status === 'passed' ? 'ok' : c.status === 'failed' ? 'rework' : 'info';

export function LineChecks() {
  const id = useId();
  const editor = useLineEditor();
  const validation = useValidation();
  const { line, stations, board } = editor;

  const rows: Row[] = [];
  for (const c of validation?.checks ?? []) {
    if (c.section === 'line') rows.push({ key: `check-${c.id}`, tone: toneOf(c), text: c.summary });
  }
  for (const p of validation?.problems ?? []) {
    // A station's problems show at its row.
    if (p.section !== 'line' || p.pointer?.startsWith('/line/stations/')) continue;
    rows.push({
      key: `problem-${p.code}-${p.pointer}`,
      tone: p.severity === 'error' ? 'rework' : 'info',
      text: p.message,
    });
  }

  const noMerge = noMergeHint(line, editor.hasRepository);
  if (noMerge) rows.push({ key: 'no-merge', tone: 'info', text: noMerge });

  if (editor.place === 'wizard') {
    for (const s of stations) {
      if (s.action !== 'merge') continue;
      const before = stations.slice(0, stations.indexOf(s)).filter((x) => x.kind === 'agent');
      const builders = [...new Set(before.flatMap((x) => (x.agent ? [x.agent] : Object.values(x.route ?? {}))))];
      const who = builders.length > 0 ? builders.join(', ') : 'the agents that open it';
      rows.push({
        key: `merge-${s.id}`,
        tone: 'info',
        text: `${stationLabel(s)} is a merge step, so ${who} ${builders.length === 1 ? 'needs' : 'need'} pull request access. You’ll set that in its environment.`,
      });
    }
  }

  for (const b of board?.stations ?? []) {
    const open = b.waiting + b.in_progress;
    if (open === 0 || stations.some((s) => s.id === b.id)) continue;
    rows.push({
      key: `removed-${b.id}`,
      tone: 'info',
      text: `Removing ${b.id}: ${open === 1 ? '1 item is' : `${open} items are`} there now. Publishing asks where they go.`,
    });
  }

  return (
    <section aria-labelledby={id} className="min-w-0 overflow-hidden rounded-control border border-line bg-card">
      <header className="border-b border-line px-4 py-3">
        <h2 id={id} className={sectionHeadingClass}>
          Checks on this line
        </h2>
      </header>
      {rows.length === 0 ? (
        <p className="m-0 flex items-center gap-2 px-4 py-3 text-sm text-muted">
          <CircleAlert size={16} strokeWidth={1.5} aria-hidden="true" />
          {validation ? 'Nothing to report.' : 'Checks run once the draft is saved.'}
        </p>
      ) : (
        <ul className="m-0 list-none p-0">
          {rows.map((r) => {
            const { Icon, label, className } = icons[r.tone];
            return (
              <li
                key={r.key}
                className="grid grid-cols-[20px_minmax(0,1fr)] items-start gap-2.5 border-t border-line px-4 py-2.5 text-sm leading-normal first:border-t-0"
              >
                <Icon size={18} strokeWidth={1.75} role="img" aria-label={label} className={className} />
                <span>{r.text}</span>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
