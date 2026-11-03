// A catalog template (canvas build/add-tool): the environment it goes to, its endpoint, a secret for each
// slot, Test (the server's answer), what agents may do and the lines it adds. Add to draft writes the
// draft's environment file only; the secrets a person creates here are saved to Secrets first.
import { useMutation } from '@tanstack/react-query';
import { Check, TriangleAlert } from 'lucide-react';
import { useMemo, useState } from 'react';
import { ApiError } from '../../../api/client';
import { testEnvironment } from '../../../api/definitions';
import type { EnvironmentTestResult, ToolTemplate } from '../../../api/types';
import { useFactory } from '../../../shared/factory';
import { plural } from '../../../shared/format';
import { Button } from '../../../ui/button';
import { Field, Input } from '../../../ui/field';
import { refusalText } from '../../connections/words';
import { useDraftSession } from '../draft/use-draft';
import { fillAdds, type ToolMode } from '../environments/env-file';
import { pickedNames, type SlotPick, suggested } from './apply-template';
import {
  AddFooter,
  EnvironmentPicker,
  PanelHeader,
  Preview,
  ServerModes,
  SlotSecret,
  useCreateSecrets,
} from './tool-parts';

const plainValue = (v: string) => !v.includes('{{');

export function TemplatePanel({
  template,
  environment,
  onEnvironment,
}: {
  template: ToolTemplate;
  environment: string;
  onEnvironment: (name: string) => void;
}) {
  const factory = useFactory();
  const session = useDraftSession();
  const create = useCreateSecrets();
  const servers = Object.entries(template.adds.mcp);
  const remote = servers.filter(([, s]) => s.url);
  const [url, setUrl] = useState(remote[0]?.[1].url ?? '');
  const [picks, setPicks] = useState<Record<string, SlotPick>>(() => suggested(template));
  const [plain, setPlain] = useState<Record<string, string>>({});
  const [modes, setModes] = useState<Record<string, Record<string, ToolMode>>>(() =>
    Object.fromEntries(servers.map(([name, s]) => [name, { ...s.tools } as Record<string, ToolMode>])),
  );
  const names = pickedNames(template, picks);
  const plainEnv = Object.entries(template.adds.env).filter(([, v]) => plainValue(v));

  const envValues = useMemo(
    () => ({ ...template.adds.env, ...Object.fromEntries(plainEnv.map(([k, v]) => [k, plain[k] ?? v])) }),
    [template, plainEnv, plain],
  );
  const adds = useMemo(
    () => fillAdds({ ...template.adds, env: envValues }, names ?? {}, { ...(url ? { url } : {}), modes }),
    [template, envValues, names, url, modes],
  );

  const test = useMutation({
    mutationFn: async (): Promise<EnvironmentTestResult> => {
      if (!names) throw new Error('Pick a secret for each slot first.');
      await create(
        Object.values(picks).flatMap((p) => (p.kind === 'new' ? [{ name: p.name.trim(), value: p.value }] : [])),
      );
      await session.flush();
      return testEnvironment(session.org, session.factory, environment, {
        template: {
          name: template.name,
          secrets: names,
          env: Object.fromEntries(plainEnv.map(([k, v]) => [k, plain[k] ?? v])),
        },
      });
    },
    gcTime: 0,
    // The secrets are in Secrets now: the picks name them as existing ones, the pasted values are dropped.
    onSuccess: () =>
      setPicks((cur) =>
        Object.fromEntries(
          Object.entries(cur).map(([k, p]) => [k, { kind: 'existing', name: p.name.trim() } as SlotPick]),
        ),
      ),
  });

  const answer = test.data;
  const serverAnswer = answer?.servers[0];
  const testable = remote.length > 0 || template.test !== null;
  const foundTools = serverAnswer?.ok ? serverAnswer.tools : undefined;
  const ok = answer ? answer.servers.every((s) => s.ok || !s.tested) && (answer.template?.ok ?? true) : undefined;
  const failure = answer?.servers.find((s) => !s.ok && s.tested)?.error ?? answer?.template?.error;

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <PanelHeader mark={template.title.slice(0, 2)} title={template.title} description={template.description} />
      <div className="grid gap-4 md:grid-cols-2">
        <EnvironmentPicker value={environment} onChange={onEnvironment} />
        {remote.length > 0 ? (
          <Field label="Endpoint">
            <Input value={url} className="font-mono" spellCheck={false} onChange={(e) => setUrl(e.target.value)} />
          </Field>
        ) : null}
      </div>
      {plainEnv.length > 0 ? (
        <div className="grid gap-4 md:grid-cols-2">
          {plainEnv.map(([key, value]) => (
            <Field key={key} label={key}>
              <Input
                value={plain[key] ?? value}
                className="font-mono"
                spellCheck={false}
                onChange={(e) => setPlain((cur) => ({ ...cur, [key]: e.target.value }))}
              />
            </Field>
          ))}
        </div>
      ) : null}
      {template.secrets.map((slot) => (
        <SlotSecret
          key={slot.slot}
          slot={slot}
          label={template.secrets.length > 1 ? slot.title : 'Secret'}
          pick={picks[slot.slot] ?? { kind: 'new', name: slot.suggest, value: '' }}
          factoryName={factory.name}
          onChange={(p) => setPicks((cur) => ({ ...cur, [slot.slot]: p }))}
        />
      ))}
      {testable ? (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-3">
            <Button
              variant="white"
              disabled={!names || test.isPending}
              aria-busy={test.isPending || undefined}
              onClick={() => test.mutate()}
            >
              {test.isPending ? 'Testing…' : answer ? 'Test again' : 'Test'}
            </Button>
            {!names ? <span className="text-secondary text-muted">Pick or paste a secret to test.</span> : null}
          </div>
          <div role="status" aria-live="polite">
            {answer ? (
              <p
                className={
                  ok
                    ? 'm-0 flex items-start gap-3 rounded-control border border-line px-4 py-3 text-body'
                    : 'm-0 flex items-start gap-3 rounded-control border border-rework bg-rework-tint px-4 py-3 text-body text-rework-ink'
                }
              >
                {ok ? (
                  <Check size={16} strokeWidth={1.75} aria-hidden="true" className="mt-0.5 shrink-0 text-ok-ink" />
                ) : (
                  <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" className="mt-0.5 shrink-0" />
                )}
                <span>
                  <span className="font-control">
                    {template.title} {ok ? 'answered' : 'did not answer'}.
                  </span>{' '}
                  {ok
                    ? foundTools
                      ? `${plural(foundTools.length, 'tool')} found.`
                      : 'The test request passed.'
                    : (failure ?? 'No answer.')}
                </span>
              </p>
            ) : null}
            {test.isError ? (
              <p className="m-0 text-body text-rework-ink">
                {test.error instanceof ApiError ? refusalText(test.error) : test.error.message}
              </p>
            ) : null}
          </div>
        </div>
      ) : null}
      {servers.map(([name]) => (
        <ServerModes
          key={name}
          server={name}
          tools={modes[name] ?? {}}
          found={name === serverAnswer?.server ? foundTools : undefined}
          onChange={(next) => setModes((cur) => ({ ...cur, [name]: next }))}
        />
      ))}
      <Preview environment={environment} adds={adds} />
      <AddFooter
        apply={{
          environment,
          adds,
          creates: Object.values(picks).flatMap((p) =>
            p.kind === 'new' ? [{ name: p.name.trim(), value: p.value }] : [],
          ),
          title: template.title,
          disabled: !names,
          reason: 'Pick a secret for each slot, or paste a new one.',
        }}
      />
    </div>
  );
}
