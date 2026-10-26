// What each editor section renders (D §10.1): the slot D7–D11 fill. A section whose view is null
// shows LaterSection; the YAML view (D10) replaces the section's view when `?mode=yaml`.
import type { ComponentType } from 'react';
import { lazyRetrying } from '../../shared/lazy';
import { BasicsSection } from './basics/basics-section';
import { LineSection } from './line/line-section';
import type { DefinitionSearch, EditorSection } from './sections';

export interface SectionViewProps {
  /** Definition edits an existing factory's draft; the wizard a new factory's (no factory id, step nav). */
  place: 'definition' | 'wizard';
  /** The section route's search params: the entity to show or focus (?station=, ?agent=, …). */
  search: DefinitionSearch;
}

// Sections with heavy editors load on first use, outside the initial bundle (D §10.9's budget);
// a chunk that fails to arrive is asked for again (shared/lazy.ts).
const AgentsSection = lazyRetrying(() => import('./agents/agents-section').then((m) => m.AgentsSection));
const ConnectionsSection = lazyRetrying(() =>
  import('./connections/connections-section').then((m) => m.ConnectionsSection),
);
const ReviewSection = lazyRetrying(() => import('./review/review-section').then((m) => m.ReviewSection));
const VersionsSection = lazyRetrying(() => import('./versions/versions-section').then((m) => m.VersionsSection));

export const sectionViews: Record<EditorSection, ComponentType<SectionViewProps> | null> = {
  basics: BasicsSection,
  line: LineSection,
  agents: AgentsSection,
  connections: ConnectionsSection,
  review: ReviewSection,
  versions: VersionsSection,
};

/** The YAML view of the whole draft (D10), with Monaco and monaco-yaml in chunks of their own. */
export const yamlView: ComponentType<SectionViewProps> | null = lazyRetrying(() =>
  import('./yaml/yaml-view').then((m) => m.YamlView),
);
