// "Where agents connect from" (canvas "Models and policies"): the addresses agents' traffic leaves from, as the
// install lists them (GOAL_EGRESS_ADDRESSES), for the operator to allow in their firewalls. Read only: the
// install sets them (ADR 0024). "Copy all" copies them one to a line.
import { SectionCard } from '../../ui/card';
import { CopyButton } from '../../ui/copy-button';

export function EgressCard({ addresses }: { addresses: string[] }) {
  return (
    <SectionCard
      title="Where agents connect from"
      aside={
        <CopyButton variant="white" label="Copy all" text={addresses.join('\n')} disabled={addresses.length === 0} />
      }
    >
      <p className="m-0 text-secondary text-ink-soft">Allow these addresses in firewalls your agents must reach.</p>
      {addresses.length === 0 ? (
        <p className="m-0 text-secondary text-muted">Your operator has not listed these addresses yet.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col overflow-hidden rounded-cell border border-line p-0">
          {addresses.map((address) => (
            <li key={address} className="border-t border-line px-3 py-2 font-mono text-secondary first:border-t-0">
              {address}
            </li>
          ))}
        </ul>
      )}
    </SectionCard>
  );
}
