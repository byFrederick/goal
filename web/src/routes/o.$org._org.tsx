import { createFileRoute, Outlet } from '@tanstack/react-router';
import { ShellFrame } from '../app/app-shell';
import { OrgSidebar } from '../app/org-sidebar';

// The organization's own pages (Factories home; Settings, and phase 3's catalog and credentials,
// add route files under this layout): the organization sidebar, no tab bar on phones.
export const Route = createFileRoute('/o/$org/_org')({
  component: OrgPagesLayout,
});

function OrgPagesLayout() {
  const { org } = Route.useRouteContext();
  return (
    <ShellFrame sidebar={<OrgSidebar org={org} />}>
      <Outlet />
    </ShellFrame>
  );
}
