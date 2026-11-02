// The tools an MCP connection's agents may use (ADR 0031, decision 1): one Allow / Ask first / Off per
// tool. A change sends the whole list, because the API replaces every mode, and the card reads the
// connection again after each save, so a mode another person set is never sent back stale.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useId } from 'react';
import { updateConnection } from '../../api/connections';
import { keys } from '../../api/queries';
import type { Connection } from '../../api/types';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { SegmentedControl, type SegmentedOption } from '../../ui/segmented-control';
import { refusalText } from './words';

/** One tool of the connection and its mode, as the contract names them. */
type ConnectionTool = Connection['tools'][number];
type ConnectionToolMode = ConnectionTool['mode'];

const modes: readonly SegmentedOption<ConnectionToolMode>[] = [
  { value: 'allow', label: 'Allow' },
  { value: 'ask', label: 'Ask first' },
  { value: 'off', label: 'Off' },
];

/** The tools of one connection, one row each, with the three-way setting; read-only without connection:manage. */
export function ToolsCard({ connection: c, manage }: { connection: Connection; manage: boolean }) {
  const org = useOrg();
  const id = useId();
  const client = useQueryClient();
  const save = useMutation({
    mutationFn: (tools: ConnectionTool[]) => updateConnection(org.id, c.name, { tools }),
    onSettled: async () => {
      // The detail and the list both show the modes and the writes waiting: read them again.
      await client.invalidateQueries({ queryKey: keys.connections(org.id) });
    },
  });

  const setMode = (tool: string, mode: ConnectionToolMode) =>
    save.mutate(c.tools.map((t) => ({ name: t.name, mode: t.name === tool ? mode : t.mode })));

  return (
    <section
      aria-labelledby={`${id}-tools`}
      className="flex min-w-0 flex-col gap-3 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <h2 id={`${id}-tools`} className="m-0 text-heading">
        Tools agents may use <span className="text-secondary font-medium text-muted">{c.tools.length} found</span>
      </h2>
      {save.error ? (
        <div role="alert" className="flex flex-col items-start gap-2">
          <Callout tone="rework">{refusalText(save.error)}</Callout>
          <Button
            variant="white"
            className="h-11 md:h-10"
            disabled={!manage || save.isPending}
            onClick={() => save.variables && save.mutate(save.variables)}
          >
            Try again
          </Button>
        </div>
      ) : null}
      {c.tools.length === 0 ? (
        <p className="m-0 text-secondary text-muted">
          No tools yet. Test the connection to list the tools its server offers.
        </p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {c.tools.map((t) => (
            <li
              key={t.name}
              className="flex flex-wrap items-center justify-between gap-2 border-t border-line py-2 first:border-t-0 first:pt-0"
            >
              <code translate="no" className="min-w-0 break-all font-mono text-secondary text-ink">
                {t.name}
              </code>
              <SegmentedControl
                label={t.name}
                options={modes}
                value={t.mode}
                readOnly={!manage || save.isPending}
                onChange={(mode) => setMode(t.name, mode)}
              />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
