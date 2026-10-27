// Personal tokens on the account page (spec 3 §10.2, PR 3-12b): "Personal tokens", the caller's own
// in this organization, with New token (personal_token:manage) and Revoke… (no recent sign-in
// needed, so a leaked token goes at once). Live tokens are listed, and greyed, those revoked in the
// last 30 days (counted, not shown, on phones). A link to #personal-tokens (the account menu's)
// scrolls there and puts focus on its heading. The members' tokens are on the API keys page.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation } from '@tanstack/react-router';
import { useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { revokePersonalToken } from '../../api/keys';
import { factoriesQuery, keys, meQuery, personalTokensQuery } from '../../api/queries';
import type { CreatedPersonalToken, OrgSummary, PersonalToken } from '../../api/types';
import { useOrgGrants, useOrgHolds } from '../../shared/org';
import { CreatedTokenDialog } from '../../shared/tokens/created-dialog';
import { RevokeDialog, TokenList } from '../../shared/tokens/token-list';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { NewTokenDialog } from './new-token-dialog';

/** The id the account menu's link names. */
export const personalTokensAnchor = 'personal-tokens';

/** The caller's own tokens, New token and Revoke…. */
export function PersonalTokensSection({ org }: { org: OrgSummary }) {
  const client = useQueryClient();
  const grants = useOrgGrants();
  const manage = useOrgHolds('personal_token:manage');
  const { data: signedIn } = useQuery({ ...meQuery, select: (me) => me.mode === 'session' });
  const list = useQuery({ ...personalTokensQuery(org.id), enabled: signedIn === true });
  const factories = useQuery(factoriesQuery(org.id));
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<CreatedPersonalToken | null>(null);
  const [revoking, setRevoking] = useState<PersonalToken | null>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const hash = useLocation({ select: (l) => l.hash });

  useEffect(() => {
    if (hash.replace(/^#/, '') !== personalTokensAnchor || !heading.current) return;
    heading.current.scrollIntoView({ block: 'start' });
    heading.current.focus({ preventScroll: true });
  }, [hash]);

  const revoke = useMutation({
    mutationFn: (t: PersonalToken) => revokePersonalToken(org.id, t.id),
    onSuccess: async (_, t) => {
      setRevoking(null);
      toast.success(`Revoked ${t.name}`, { description: 'Anything using it stops working at once.' });
      await Promise.all([
        client.invalidateQueries({ queryKey: keys.personalTokens(org.id) }),
        client.invalidateQueries({ queryKey: keys.memberTokens(org.id) }),
      ]);
    },
  });

  const newToken =
    manage && signedIn ? (
      <Button variant="white" className="h-11 md:h-10" onClick={() => setCreating(true)}>
        <span aria-hidden="true">+</span> New token
      </Button>
    ) : null;

  return (
    <section
      id={personalTokensAnchor}
      aria-labelledby="personal-tokens-heading"
      className="flex scroll-mt-6 flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex max-w-[680px] min-w-0 flex-col gap-2">
          <h2 id="personal-tokens-heading" ref={heading} tabIndex={-1} className="m-0 text-xl font-medium outline-none">
            Personal tokens
          </h2>
          <p className="m-0 text-sm leading-normal text-muted">
            A personal token lets the CLI or a script on your machine act as you in {org.name}, with no more than your
            role allows. Tokens expire after at most 90 days.
          </p>
        </div>
        {newToken}
      </div>

      {signedIn === false ? (
        <Callout tone="note">
          Personal tokens need sign-in. With sign-in off, the CLI acts as the local user without one.
        </Callout>
      ) : (
        <TokenList
          list={list}
          what="your personal tokens"
          caption={`Your personal tokens in ${org.name}`}
          empty="No personal tokens. Use goal login for the CLI, or create a token for a headless machine."
          onRevoke={setRevoking}
        />
      )}

      {creating ? (
        <NewTokenDialog
          org={org}
          orgCan={grants?.can}
          factories={factories.data ?? []}
          onClose={() => setCreating(false)}
          onCreated={(t) => {
            setCreating(false);
            setCreated(t);
            void client.invalidateQueries({ queryKey: keys.personalTokens(org.id) });
            void client.invalidateQueries({ queryKey: keys.memberTokens(org.id) });
          }}
        />
      ) : null}
      <CreatedTokenDialog created={created} orgName={org.name} onDone={() => setCreated(null)} />
      <RevokeDialog
        token={revoking}
        title={`Revoke ${revoking?.name ?? 'token'}?`}
        pending={revoke.isPending}
        error={revoke.error}
        onCancel={() => {
          setRevoking(null);
          revoke.reset();
        }}
        onConfirm={() => {
          if (revoking) revoke.mutate(revoking);
        }}
      />
    </section>
  );
}
