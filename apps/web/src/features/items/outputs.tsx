// Station outputs are whatever each station's contract says, so they are rendered generically:
// prose fields as text, lists as lists, a pull request as a link, anything nested as JSON.
import { ExternalLink } from 'lucide-react';
import type { Factory } from '../../api/types';
import { humanize } from '../../shared/format';
import { pullRequestLabel, pullRequestsIn } from '../../shared/item-state';
import { SectionCard } from '../../ui/card';

const prose = ['summary', 'description', 'diagnosis', 'reason', 'text'];

/** The prose an output leads with (its summary, description, …), if it has one. */
export function outputLead(value: unknown): string | undefined {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined;
  const obj = value as Record<string, unknown>;
  const key = prose.find((k) => typeof obj[k] === 'string');
  return key ? String(obj[key]) : undefined;
}

function Value({ name, value }: { name: string; value: unknown }) {
  if (Array.isArray(value) && value.every((v) => typeof v === 'string')) {
    return (
      <div className="flex flex-col gap-1">
        <span className="text-sm text-muted">{humanize(name)}</span>
        <ul className="m-0 flex list-disc flex-col gap-1 pl-5 text-[15px] leading-normal">
          {value.map((v) => (
            <li key={v}>{v}</li>
          ))}
        </ul>
      </div>
    );
  }
  if (value !== null && typeof value === 'object') {
    return (
      <div className="flex flex-col gap-1">
        <span className="text-sm text-muted">{humanize(name)}</span>
        <pre className="m-0 overflow-x-auto rounded-control bg-shell p-3 text-[13px] leading-normal">
          {JSON.stringify(value, null, 2)}
        </pre>
      </div>
    );
  }
  const text = String(value);
  const url = /^https?:\/\//.test(text);
  return (
    <div className="flex items-baseline justify-between gap-3 border-t border-line pt-2 text-[15px]">
      <span className="text-muted">{humanize(name)}</span>
      {url ? (
        <a href={text} target="_blank" rel="noreferrer" className="[overflow-wrap:anywhere] hover:text-run">
          {text}
        </a>
      ) : (
        <span className={name === 'branch' || name === 'lane' ? 'font-mono text-[13px]' : ''}>{text}</span>
      )}
    </div>
  );
}

function Output({ station, value }: { station: string; value: unknown }) {
  const obj =
    value !== null && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
  const prs = pullRequestsIn(value);
  const lead = obj ? prose.find((k) => typeof obj[k] === 'string') : undefined;
  const linked = new Set(['pull_request', 'pull_requests', 'pr_url']);
  const rest = obj ? Object.entries(obj).filter(([k]) => k !== lead && !linked.has(k)) : [];

  return (
    <article className="flex flex-col gap-3 rounded-control border border-line bg-card p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="m-0 text-[15px] font-medium">{humanize(station)}</h3>
        {prs.length > 0 ? (
          <span className="flex flex-wrap gap-2">
            {prs.map((pr) => (
              <a
                key={pr}
                href={pr}
                target="_blank"
                rel="noreferrer"
                className="inline-flex h-9 items-center gap-2 rounded-control bg-panel px-3 text-sm no-underline hover:bg-hover"
              >
                {pullRequestLabel(pr, prs.length > 1)}
                <ExternalLink size={14} strokeWidth={1.5} aria-hidden="true" />
                <span className="sr-only">(opens in a new tab)</span>
              </a>
            ))}
          </span>
        ) : null}
      </div>
      {obj ? (
        <>
          {lead ? <p className="m-0 text-[17px] leading-normal tracking-[-0.01em]">{String(obj[lead])}</p> : null}
          {rest.map(([k, v]) => (
            <Value key={k} name={k} value={v} />
          ))}
        </>
      ) : (
        <p className="m-0 text-[15px] leading-normal whitespace-pre-wrap">{String(value)}</p>
      )}
    </article>
  );
}

/** Every station's output, in line order. */
export function OutputsCard({
  factory,
  outputs,
  className,
}: {
  factory: Factory;
  outputs: Record<string, unknown>;
  className?: string;
}) {
  const order = factory.stations.map((s) => s.id);
  const stations = Object.keys(outputs).sort((a, b) => order.indexOf(a) - order.indexOf(b));
  if (stations.length === 0) return null;

  return (
    <SectionCard title="Outputs" description="What each station produced" className={className}>
      {stations.map((s) => (
        <Output key={s} station={s} value={outputs[s]} />
      ))}
    </SectionCard>
  );
}
