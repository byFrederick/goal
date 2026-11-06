// Organization settings, Notifications (ADR 0037): which of the organization's secrets holds the Slack
// bot token that sends direct messages. It saves on its own, not with the page's General and Slots form.
// Test asks Slack about the picked secret and shows the answer in place (MOTION.md, A status changes in
// place); a failed save is a toast. Without org:manage the saved name is shown as text.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../api/client';
import { setOrgNotificationSettings, testOrgNotifications } from '../../api/org-notifications';
import { keys, orgNotificationsQuery, secretsQuery } from '../../api/queries';
import type { NotificationTestResult } from '../../api/types';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { ChipText } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { Select } from '../../ui/select';
import { Skeleton } from '../../ui/skeleton';
import { SettingsRow, SettingsSection } from './card';

const defaultSecret = 'slack-bot';

/** The plain words for the codes the test answers with. */
const failures: Record<string, string> = {
  'slack-not-set': 'Pick a secret first',
  'slack-token-invalid': 'Slack refused this token',
  'slack-token-unavailable': 'Slack could not be reached. Try again.',
};

function outcome(result: NotificationTestResult): { ok: boolean; text: string } {
  if (result.ok) return { ok: true, text: `Works with ${result.workspace ?? 'Slack'}` };
  return { ok: false, text: failures[result.error ?? ''] ?? 'Slack did not accept this token' };
}

export function NotificationsSection() {
  const org = useOrg();
  const client = useQueryClient();
  const mayManage = useOrgHolds('org:manage');
  const settings = useQuery(orgNotificationsQuery(org.id));
  const secrets = useQuery(secretsQuery(org.id));
  const [picked, setPicked] = useState<string | null>(null);
  const [result, setResult] = useState<{ for: string; value: { ok: boolean; text: string } } | null>(null);

  const saved = settings.data?.slack_secret ?? null;
  const names = (secrets.data ?? []).map((s) => s.name);
  const value = picked ?? saved ?? (names.includes(defaultSecret) ? defaultSecret : '');
  const options = (value && !names.includes(value) ? [value, ...names] : names).map((name) => ({
    value: name,
    label: name,
  }));
  const shownResult = result && result.for === value ? result.value : null;

  const save = useMutation({
    mutationFn: (name: string | null) => setOrgNotificationSettings(org.id, name),
    onSuccess: (next) => {
      client.setQueryData(keys.orgNotifications(org.id), next);
      setPicked(null);
      toast.success('Saved');
    },
    onError: (err) => {
      toast.error(err instanceof ApiError ? `Not saved: ${err.detail ?? err.title}` : 'Not saved. Try again.');
    },
  });
  const test = useMutation({
    mutationFn: (name: string) => testOrgNotifications(org.id, name),
    onSuccess: (answer, name) => setResult({ for: name, value: outcome(answer) }),
    onError: (_err, name) =>
      setResult({ for: name, value: { ok: false, text: 'Slack could not be reached. Try again.' } }),
  });

  const ready = !settings.isPending && !secrets.isPending;
  const none = ready && names.length === 0 && !saved;

  return (
    <SettingsSection id="notifications" title="Notifications">
      <SettingsRow id="slack-secret" label="Slack" help="Direct messages from your Slack bot">
        <Skeleton
          ready={ready}
          placeholder={
            <div className="flex flex-wrap items-center gap-2">
              <div className="h-10 w-60 rounded-control bg-panel" />
              <div className="h-10 w-16 rounded-control bg-panel" />
            </div>
          }
        >
          {!ready ? null : !mayManage ? (
            <span className={cn('text-secondary', saved ? 'font-mono text-ink' : 'text-muted')}>{saved ?? 'Off'}</span>
          ) : none ? (
            <Link
              to="/o/$org/secrets"
              params={{ org: org.slug }}
              search={{ new: true }}
              className="w-fit text-secondary text-ink underline underline-offset-2"
            >
              New secret
            </Link>
          ) : (
            <div className="flex flex-col gap-2">
              <div className="flex flex-wrap items-center gap-2">
                <Select
                  id="slack-secret"
                  aria-label="Slack secret"
                  value={value}
                  placeholder="Pick a secret"
                  className="w-60 max-w-full font-mono"
                  options={options}
                  onValueChange={setPicked}
                />
                <Button
                  type="button"
                  size="md"
                  busy={test.isPending}
                  busyLabel="Testing…"
                  disabled={!value}
                  onClick={() => test.mutate(value)}
                >
                  Test
                </Button>
                <Button
                  type="button"
                  variant="action"
                  size="md"
                  busy={save.isPending && save.variables !== null}
                  busyLabel="Saving…"
                  disabled={!value || value === saved}
                  onClick={() => save.mutate(value)}
                >
                  Save
                </Button>
                {saved ? (
                  <Button
                    type="button"
                    size="md"
                    busy={save.isPending && save.variables === null}
                    busyLabel="Turning off…"
                    onClick={() => save.mutate(null)}
                  >
                    Turn off
                  </Button>
                ) : null}
              </div>
              {shownResult ? (
                <span
                  role="status"
                  className={cn(
                    'inline-flex items-center gap-2 text-secondary',
                    shownResult.ok ? 'text-ink-soft' : 'text-rework-ink',
                  )}
                >
                  <span
                    aria-hidden="true"
                    className={cn('size-1.5 shrink-0 rounded-full', shownResult.ok ? 'bg-ok' : 'bg-rework')}
                  />
                  <ChipText text={shownResult.text} />
                </span>
              ) : null}
            </div>
          )}
        </Skeleton>
      </SettingsRow>
    </SettingsSection>
  );
}
