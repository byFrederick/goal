// New credential (ADR 0017, canvas "New credential"). Only the route is here so the New credential
// menu can link to it: the form and the import panel (?panel=import) are built by the item that
// follows this one, which replaces this body. Until then the page says what is not built yet.
import { useSearch } from '@tanstack/react-router';
import { Button } from '../../ui/button';
import { NotAvailable } from '../../ui/not-available';
import { PageHeader } from '../../ui/page-header';

export function NewCredentialPage() {
  const { panel } = useSearch({ from: '/o/$org/_org/credentials/new' });
  const importing = panel === 'import';
  return (
    <>
      <PageHeader title={importing ? 'Import credentials' : 'New credential'} />
      <NotAvailable label={importing ? 'Import from .env or YAML' : 'Create a credential'}>
        <Button variant="primary">{importing ? 'Check and import' : 'Create credential'}</Button>
      </NotAvailable>
    </>
  );
}
