// What a synced source reads (its config), by the reader its connection's kind syncs with (spec 4
// §4.8): gitdocs takes a repository's https URL, a branch and path globs to include and leave out
// (include **/*.md when empty); confluence takes space keys. Globs and keys are separated by
// commas or new lines, as goald reads them.
import { defaultInclude } from '../../shared/knowledge';
import { Field, Input } from '../../ui/field';

export type Config = Record<string, string>;

interface FieldSpec {
  name: string;
  label: string;
  required?: boolean;
  placeholder?: string;
  help: (connection: string) => string;
}

const readers: Record<string, FieldSpec[]> = {
  gitdocs: [
    {
      name: 'repository',
      label: 'Repository',
      required: true,
      placeholder: 'https://github.com/acme/docs',
      help: (c) => `Its https URL. Read with ${c}’s token.`,
    },
    { name: 'branch', label: 'Branch (optional)', placeholder: 'main', help: () => 'Empty means its default branch.' },
    {
      name: 'include',
      label: 'Paths',
      placeholder: defaultInclude,
      help: () => `Globs, separated by commas. Empty means ${defaultInclude}.`,
    },
    {
      name: 'exclude',
      label: 'Leave out (optional)',
      placeholder: 'docs/archive/**',
      help: () => 'Globs of files to skip, separated by commas.',
    },
  ],
  confluence: [
    {
      name: 'spaces',
      label: 'Spaces',
      required: true,
      placeholder: 'ENG, SUP',
      help: (c) => `Space keys, separated by commas. Read with ${c}’s account.`,
    },
  ],
};

/** The reader a source's config is for: its connection kind's sync, else what its keys say. */
export function readerOf(sync: string | undefined, config: Config): string | undefined {
  if (sync && readers[sync]) return sync;
  if ('repository' in config) return 'gitdocs';
  if ('spaces' in config) return 'confluence';
  return undefined;
}

/** Why the config can't be saved, by field; empty when it can. */
export function configErrors(reader: string | undefined, config: Config): Record<string, string> {
  const errors: Record<string, string> = {};
  for (const f of readers[reader ?? ''] ?? []) {
    const value = (config[f.name] ?? '').trim();
    if (f.required && value === '') errors[f.name] = `${f.label} is required.`;
  }
  const repository = (config.repository ?? '').trim();
  if (reader === 'gitdocs' && repository !== '' && !/^https:\/\/[^/\s]+\/\S+$/.test(repository)) {
    errors.repository = 'An https URL, such as https://github.com/acme/docs.';
  }
  return errors;
}

/** The config without empty values, as goald stores it. */
export function trimmed(config: Config): Config {
  return Object.fromEntries(
    Object.entries(config)
      .map(([k, v]) => [k, v.trim()] as const)
      .filter(([, v]) => v !== ''),
  );
}

export function ConfigFields({
  reader,
  connection,
  config,
  errors,
  onChange,
}: {
  reader: string | undefined;
  connection: string;
  config: Config;
  errors: Record<string, string>;
  onChange: (config: Config) => void;
}) {
  return (
    <>
      {(readers[reader ?? ''] ?? []).map((f) => (
        <Field key={f.name} label={f.label} help={f.help(connection)} error={errors[f.name]}>
          <Input
            value={config[f.name] ?? ''}
            placeholder={f.placeholder}
            onChange={(e) => onChange({ ...config, [f.name]: e.target.value })}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            translate="no"
            className="font-mono md:text-sm"
          />
        </Field>
      ))}
    </>
  );
}
