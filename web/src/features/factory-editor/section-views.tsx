// What each editor section renders (D §10.1): the slot D7–D11 fill. A section whose view is null
// shows LaterSection; the YAML view (D10) replaces the section's view when `?mode=yaml`.
import { type ComponentType, lazy } from 'react';
import { BasicsSection } from './basics/basics-section';
import { LineSection } from './line/line-section';
import type { DefinitionSearch, EditorSection } from './sections';

export interface SectionViewProps {
  /** Definition edits an existing factory's draft; the wizard a new factory's (no factory id, step nav). */
  place: 'definition' | 'wizard';
  /** The section route's search params: the entity to show or focus (?station=, ?agent=, …). */
  search: DefinitionSearch;
}

// Sections with heavy editors load on first use, outside the initial bundle (D §10.9's budget).
const AgentsSection = lazy(() => import('./agents/agents-section').then((m) => ({ default: m.AgentsSection })));
const ConnectionsSection = lazy(() =>
  import('./connections/connections-section').then((m) => ({ default: m.ConnectionsSection })),
);

export const sectionViews: Record<EditorSection, ComponentType<SectionViewProps> | null> = {
  basics: BasicsSection,
  line: LineSection,
  agents: AgentsSection,
  connections: ConnectionsSection,
  review: null,
  versions: null,
};

/** The YAML view of the whole draft (D10), with Monaco and monaco-yaml in chunks of their own. */
export const yamlView: ComponentType<SectionViewProps> | null = lazy(() =>
  import('./yaml/yaml-view').then((m) => ({ default: m.YamlView })),
);
