import { createFileRoute } from '@tanstack/react-router';
import { pageTitle } from '../app/route-focus';
import { PendingScreen } from '../app/states';
import { AddToolPage, validateAddToolSearch } from '../features/factory-editor/add-tool/add-tool-page';

// Add a tool: the catalog of MCP servers, command-line tools and APIs, writing into the draft's
// environment file (?environment=, ?template=<name or custom>, ?type=mcp|cli|api).
export const Route = createFileRoute('/o/$org/f/$factory/definition_/add-tool')({
  pendingComponent: PendingScreen,
  validateSearch: validateAddToolSearch,
  head: ({ match: { context } }) => pageTitle('Add a tool', 'Definition', context.factory.name, context.org.name),
  component: AddToolPage,
});
