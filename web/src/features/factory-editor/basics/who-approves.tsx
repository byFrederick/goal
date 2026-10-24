// Basics › Who approves (spec 3 §10.2, canvas Define-Basics and New-Basics): read-only, since who
// approves is not part of the definition but of the factory's roles. Owners and admins of the
// organization approve everywhere; a factory's approvers and editors approve in it. A new factory
// has none of its own yet, so its owners and admins approve until someone is given the role.
import { useOrg } from '../../../shared/org';

export function WhoApproves({ place }: { place: 'definition' | 'wizard' }) {
  const org = useOrg();
  return (
    <div className="col-span-full flex min-w-0 flex-col gap-1.5">
      <h3 className="m-0 text-sm font-medium">Who approves</h3>
      <p className="m-0 text-[15px] leading-normal">
        {place === 'wizard'
          ? 'Owners and admins approve until you add approvers in Members.'
          : `Owners and admins of ${org.name}, and this factory’s approvers and editors.`}
      </p>
      <p className="m-0 text-[13px] leading-snug text-muted">
        Human stations and write actions wait for one of them. Approvers and editors are factory roles, set per person
        in Members and roles.
      </p>
    </div>
  );
}
