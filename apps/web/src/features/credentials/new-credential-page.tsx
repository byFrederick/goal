// New credential (ADR 0017, canvas docs/design/screens/connections/credential-new.jpg): create one
// credential, or import many from a .env or YAML text. ?panel=import opens the import panel, which the
// Credentials list's second menu entry links to. The top bar carries the crumb and Cancel (TopBarSlot).
import { useQuery } from '@tanstack/react-query';
import { Link, useSearch } from '@tanstack/react-router';
import { factoriesQuery } from '../../api/queries';
import { TopBarSlot } from '../../app/top-bar';
import { useOrg, useOrgHolds } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Skeleton } from '../../ui/skeleton';
import { CreateCredentialCard } from './create-credential-card';
import { ImportCredentialsCard } from './import-credentials-card';

export function NewCredentialPage() {
  const org = useOrg();
  const { panel } = useSearch({ from: '/o/$org/_org/credentials/new' });
  const manage = useOrgHolds('secret:manage');
  const factories = useQuery(factoriesQuery(org.id));
  const importing = panel === 'import';

  return (
    <>
      <TopBarSlot
        crumb="New credential"
        actions={
          <Button asChild variant="white">
            <Link to="/o/$org/credentials" params={{ org: org.slug }}>
              Cancel
            </Link>
          </Button>
        }
      />
      <h1 className="m-0 text-page-title">{importing ? 'Import credentials' : 'New credential'}</h1>

      {!manage ? (
        <Callout tone="note">Owners and admins of {org.name} add credentials.</Callout>
      ) : factories.isPending ? (
        <Skeleton
          ready={false}
          placeholder={
            <div className="flex flex-col gap-3 rounded-card border border-line bg-card p-5">
              <div className="h-12 rounded-control bg-hover" />
              <div className="h-12 rounded-control bg-hover" />
            </div>
          }
        >
          <span />
        </Skeleton>
      ) : factories.isError ? (
        <div
          className="flex max-w-xl flex-col items-start gap-3 rounded-card border border-line bg-card p-5"
          role="alert"
        >
          <Callout tone="rework">Couldn’t load the factories. Check your connection and try again.</Callout>
          <Button variant="white" onClick={() => void factories.refetch()}>
            Try again
          </Button>
        </div>
      ) : (
        <div className="@container">
          <div className="grid items-start gap-5 @[1100px]:grid-cols-[minmax(0,1fr)_420px]">
            <CreateCredentialCard org={org.id} slug={org.slug} factories={factories.data} />
            <ImportCredentialsCard org={org.id} slug={org.slug} focus={importing} />
          </div>
        </div>
      )}
    </>
  );
}
