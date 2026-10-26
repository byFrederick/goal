import { createFileRoute, Outlet } from '@tanstack/react-router';
import { DefinitionLayout } from '../features/factory-editor/definition-page';

// The factory's Definition (D §10.1): the frame and the draft session, around one section.
export const Route = createFileRoute('/o/$org/f/$factory/definition')({
  component: () => (
    <DefinitionLayout>
      <Outlet />
    </DefinitionLayout>
  ),
});
