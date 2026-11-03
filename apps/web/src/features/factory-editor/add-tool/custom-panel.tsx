// Custom (canvas build/add-tool-custom): what the catalog lacks, as an MCP server, a command-line tool
// or an API. Each is a short form that produces the same preview as a template, and Add to draft writes
// the same file.
import { useState } from 'react';
import { Field, Input } from '../../../ui/field';
import { SegmentedControl } from '../../../ui/segmented-control';
import { NewSecretSheetFor } from '../environments/new-secret';
import { RemoveButton, SecretSelect } from '../environments/parts';
import {
  blankCustom,
  type CustomRow,
  type CustomTool,
  customAdds,
  customProblem,
  type ToolKind,
} from './apply-template';
import { AddFooter, EnvironmentPicker, PanelHeader, Preview } from './tool-parts';

const types = [
  { value: 'mcp', label: 'MCP server' },
  { value: 'cli', label: 'Command-line tool' },
  { value: 'api', label: 'API' },
] as const;

const typeHelp: Record<ToolKind, string> = {
  mcp: 'An MCP server gives agents tools to call. Its key, if it needs one, comes from Secrets.',
  cli: 'A command-line tool is installed with mise and reads variables or files.',
  api: 'An API is just variables, such as a base URL and a key.',
};

/** Variables or files: a name, then plain text or a secret. */
function Rows({
  label,
  add,
  rows,
  files,
  onChange,
  newSecret,
}: {
  label: string;
  add: string;
  rows: CustomRow[];
  files: boolean;
  onChange: (rows: CustomRow[]) => void;
  newSecret: (done: (name: string) => void) => void;
}) {
  const set = (i: number, patch: Partial<CustomRow>) =>
    onChange(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  return (
    <fieldset className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
      <legend className="mb-2 p-0 text-body font-control">{label}</legend>
      {rows.map((row, i) => (
        <div
          // biome-ignore lint/suspicious/noArrayIndexKey: rows have no identity until they are written
          key={i}
          className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2 md:grid-cols-[minmax(0,5fr)_auto_minmax(0,7fr)_auto]"
        >
          <Input
            aria-label={`${files ? 'Path' : 'Name'} ${i + 1}`}
            value={row.key}
            className="font-mono"
            spellCheck={false}
            onChange={(e) => set(i, { key: e.target.value })}
          />
          <span aria-hidden="true" className="hidden text-muted md:block">
            =
          </span>
          <div className="order-last col-span-2 flex min-w-0 items-center gap-2 md:order-none md:col-span-1">
            {files || row.secret !== '' ? (
              <SecretSelect
                label={`Secret for ${row.key || `row ${i + 1}`}`}
                value={row.secret.trim() || undefined}
                onChange={(secret) => set(i, { secret })}
                withNew={() => newSecret((secret) => set(i, { secret }))}
                className="min-w-0"
              />
            ) : (
              <>
                <Input
                  aria-label={`Value ${i + 1}`}
                  value={row.value}
                  className="font-mono"
                  spellCheck={false}
                  onChange={(e) => set(i, { value: e.target.value })}
                />
                <button
                  type="button"
                  onClick={() => set(i, { secret: ' ', value: '' })}
                  className="press inline-flex min-h-11 shrink-0 cursor-pointer items-center whitespace-nowrap rounded-control px-2 text-secondary text-ink hover:text-link md:min-h-8"
                >
                  Use a secret
                </button>
              </>
            )}
          </div>
          <RemoveButton
            label={`Remove ${row.key || `row ${i + 1}`}`}
            onClick={() => onChange(rows.filter((_, j) => j !== i))}
          />
        </div>
      ))}
      <button
        type="button"
        onClick={() => onChange([...rows, { key: '', value: '', secret: '' }])}
        className="press inline-flex min-h-11 w-fit cursor-pointer items-center gap-2 text-body text-ink hover:text-link md:min-h-8"
      >
        <span aria-hidden="true" className="text-lg leading-none">
          +
        </span>
        {add}
      </button>
    </fieldset>
  );
}

export function CustomPanel({
  environment,
  onEnvironment,
  type,
  onType,
}: {
  environment: string;
  onEnvironment: (name: string) => void;
  type: ToolKind;
  onType: (type: ToolKind) => void;
}) {
  const [tool, setTool] = useState<CustomTool>(() => blankCustom(type));
  const secret = NewSecretSheetFor();
  const current = { ...tool, type };
  const problem = customProblem(current);
  const adds = customAdds(current);
  const title =
    type === 'mcp'
      ? current.name.trim() || 'the server'
      : type === 'cli'
        ? current.install.split('@')[0] || 'the tool'
        : 'the API';

  return (
    <div className="flex min-w-0 flex-col gap-5">
      <PanelHeader mark="<>" title="Custom tool" description="Anything the catalog doesn’t have." />
      <div className="flex flex-col gap-2">
        <SegmentedControl<ToolKind> label="Type" options={types} value={type} onChange={onType} />
        <p className="m-0 text-secondary text-muted">{typeHelp[type]}</p>
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        <EnvironmentPicker value={environment} onChange={onEnvironment} />
        {type === 'cli' ? (
          <Field label="Install" help="A mise tool spec: name@version.">
            <Input
              value={tool.install}
              className="font-mono"
              placeholder="kubectl@1.31"
              spellCheck={false}
              onChange={(e) => setTool({ ...tool, install: e.target.value })}
            />
          </Field>
        ) : null}
        {type === 'mcp' ? (
          <Field label="Name" help="Lowercase letters, digits, dashes.">
            <Input
              value={tool.name}
              className="font-mono"
              placeholder="docs"
              spellCheck={false}
              onChange={(e) => setTool({ ...tool, name: e.target.value })}
            />
          </Field>
        ) : null}
      </div>
      {type === 'mcp' ? (
        <div className="grid gap-4 md:grid-cols-2">
          <Field label="Endpoint">
            <Input
              value={tool.url}
              className="font-mono"
              placeholder="https://mcp.example.com/mcp"
              spellCheck={false}
              onChange={(e) => setTool({ ...tool, url: e.target.value })}
            />
          </Field>
          <Field
            label={
              <>
                Secret <span className="font-normal text-muted">optional</span>
              </>
            }
          >
            <SecretSelect
              label="Secret for the server"
              value={tool.token || undefined}
              placeholder="No key"
              onChange={(token) => setTool({ ...tool, token })}
              withNew={() => secret.open((token) => setTool((cur) => ({ ...cur, token })))}
            />
          </Field>
        </div>
      ) : (
        <>
          <Rows
            label="Variables"
            add="Add a variable"
            rows={tool.variables}
            files={false}
            newSecret={secret.open}
            onChange={(variables) => setTool({ ...tool, variables })}
          />
          {type === 'cli' ? (
            <Rows
              label="Files"
              add="Add a file"
              rows={tool.files}
              files
              newSecret={secret.open}
              onChange={(files) => setTool({ ...tool, files })}
            />
          ) : null}
        </>
      )}
      <Preview environment={environment} adds={adds} />
      <AddFooter apply={{ environment, adds, creates: [], title, disabled: problem !== undefined, reason: problem }} />
      {secret.sheet}
    </div>
  );
}
