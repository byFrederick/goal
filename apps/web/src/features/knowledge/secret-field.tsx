// The secret a synced source reads with (ADR 0037): the code host's token for a repository's docs, the API
// token for Confluence. The source syncs with no factory, so only secrets open to all factories are
// offered; New secret opens the Secrets sheet with that scope already chosen.
import { useQuery } from '@tanstack/react-query';
import { secretsQuery } from '../../api/queries';
import { useOrg } from '../../shared/org';
import { Field } from '../../ui/field';
import { Select } from '../../ui/select';
import { NewSecretSheetFor } from '../factory-editor/environments/new-secret';

const NEW = '__new__';

export function SecretField({
  value,
  onChange,
  error,
  help,
}: {
  value: string;
  onChange: (name: string) => void;
  error?: string | undefined;
  help?: string | undefined;
}) {
  const org = useOrg();
  const secrets = useQuery(secretsQuery(org.id)).data ?? [];
  const newSecret = NewSecretSheetFor({ allFactories: true });
  const open = secrets.filter((s) => s.scope.all_factories).map((s) => s.name);
  const names = value !== '' && !open.includes(value) ? [value, ...open] : open;
  return (
    <>
      <Field label="Secret" error={error} help={help ?? 'Open to all factories, as a source syncs without one.'}>
        <Select
          aria-label="Secret"
          value={value}
          placeholder="Pick a secret"
          className="font-mono"
          options={[...names.map((n) => ({ value: n, label: n })), { value: NEW, label: 'New secret…' }]}
          onValueChange={(v) => (v === NEW ? newSecret.open(onChange) : onChange(v))}
        />
      </Field>
      {newSecret.sheet}
    </>
  );
}
