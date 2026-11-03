// What a synced source reads (its config), by its reader (spec 4 §4.8, ADR 0037): gitdocs takes a
// repository's https URL, a branch and path globs to include and leave out (include **/*.md when empty);
// confluence takes the site, the account's email and space keys. The secret holds the token. Globs and
// keys are separated by commas or new lines, as goald reads them.
import { defaultInclude } from '../../shared/knowledge';
import { Field, Input } from '../../ui/field';

export type Config = Record<string, string>;

interface FieldSpec {
  name: string;
  label: string;
  required?: boolean;
  placeholder?: string;
  help: (secret: string) => string;
}

const readers: Record<string, FieldSpec[]> = {
  gitdocs: [
    {
      name: 'repository',
      label: 'Repository',
      required: true,
      placeholder: 'https://github.com/acme/docs',
      help: (c) => `Its https URL. Read with the token in ${c || 'the secret'}.`,
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
      name: 'site',
      label: 'Site',
      required: true,
      placeholder: 'https://acme.atlassian.net',
      help: () => 'The address of your Confluence site.',
    },
    {
      name: 'email',
      label: 'Account email',
      required: true,
      placeholder: 'bot@example.com',
      help: (c) => `The account the token in ${c || 'the secret'} belongs to.`,
    },
    {
      name: 'spaces',
      label: 'Spaces',
      required: true,
      placeholder: 'ENG, SUP',
      help: () => 'Space keys, separated by commas.',
    },
  ],
};

/** The readers a synced source can use, with how they read to a person. */
export const readerChoices = [
  { value: 'gitdocs', label: 'Repository docs' },
  { value: 'confluence', label: 'Confluence' },
] as const;

/** The reader a source's config is for: the one it names, else what a connection kind syncs with, else what its keys say. */
export function readerOf(sync: string | undefined, config: Config): string | undefined {
  if (config.reader && readers[config.reader]) return config.reader;
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
  secret,
  config,
  errors,
  onChange,
}: {
  reader: string | undefined;
  secret: string;
  config: Config;
  errors: Record<string, string>;
  onChange: (config: Config) => void;
}) {
  return (
    <>
      {(readers[reader ?? ''] ?? []).map((f) => (
        <Field key={f.name} label={f.label} help={f.help(secret)} error={errors[f.name]}>
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
