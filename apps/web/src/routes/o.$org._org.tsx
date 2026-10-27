import { createFileRoute, Outlet } from '@tanstack/react-router';
import { ShellFrame } from '../app/app-shell';

// The organization's own pages (Factories home; Settings, and phase 4's catalog and credentials,
// add route files under this layout): no tab bar on phones. The sidebar is AppShell's.
export const Route = createFileRoute('/o/$org/_org')({
  component: OrgPagesLayout,
});

function OrgPagesLayout() {
  return (
    <ShellFrame>
      <Outlet />
    </ShellFrame>
  );
}
