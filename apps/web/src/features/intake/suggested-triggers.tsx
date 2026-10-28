// Suggested triggers (intake): a catalog of starting points under tabs. The tab is in the URL (the page
// passes it in), so a link opens the same tab. Add goes to the trigger page with the choice in the address;
// a suggestion whose event the schema does not accept is listed disabled.
import { useNavigate } from '@tanstack/react-router';
import { ArrowRight } from 'lucide-react';
import { factoryTitle, useFactory, useFactoryParams } from '../../shared/factory';
import { Button } from '../../ui/button';
import { Chip } from '../../ui/chip';
import { NotAvailable } from '../../ui/not-available';
import { SegmentedControl } from '../../ui/segmented-control';
import { sourceByKey } from './sources';
import {
  isChoosable,
  type Suggestion,
  type SuggestionGroup,
  suggestionGroups,
  suggestionHref,
  suggestionLabels,
  suggestions,
} from './suggestions';

export function SuggestedTriggers({
  group,
  onGroupChange,
}: {
  group: SuggestionGroup;
  onGroupChange: (group: SuggestionGroup) => void;
}) {
  const factory = useFactory();
  const options = suggestionGroups.map((value) => ({
    value,
    label: value === 'software' ? `For ${factoryTitle(factory)}` : suggestionLabels[value],
  }));
  const shown = suggestions.filter((s) => s.group === group);

  return (
    <section aria-labelledby="suggested-heading" className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <h2 id="suggested-heading" className="m-0 text-heading">
          Suggested triggers
        </h2>
        <SegmentedControl label="Suggested for" options={options} value={group} onChange={onGroupChange} />
      </div>
      {shown.length === 0 ? (
        <p className="m-0 text-secondary text-muted">No suggestions for this group yet.</p>
      ) : (
        <ul className="m-0 grid list-none gap-3 p-0 md:grid-cols-2 xl:grid-cols-3">
          {shown.map((suggestion) => (
            <li key={suggestion.id}>
              <SuggestionCard suggestion={suggestion} />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function SuggestionCard({ suggestion }: { suggestion: Suggestion }) {
  const navigate = useNavigate();
  const params = useFactoryParams();
  const Icon = sourceByKey[suggestion.source].icon;
  const href = suggestionHref(params.org, params.factory, suggestion);
  const add = (
    <Button size="sm" onClick={() => navigate({ href })} className="self-start">
      Add
    </Button>
  );

  return (
    <article className="flex h-full flex-col gap-2 rounded-card border border-line p-4">
      <div className="flex items-center gap-2 text-muted" aria-hidden="true">
        {Icon ? (
          <span className="flex size-6 items-center justify-center rounded-cell bg-panel text-ink-soft">
            <Icon className="size-3.5" />
          </span>
        ) : null}
        <ArrowRight className="size-3.5" />
        <Chip>{suggestion.station}</Chip>
      </div>
      <h3 className="m-0 text-heading">{suggestion.title}</h3>
      <p className="m-0 text-secondary text-ink-soft">{suggestion.body}</p>
      <div className="mt-auto pt-1">
        {isChoosable(suggestion) ? (
          add
        ) : (
          <NotAvailable label={suggestion.title}>
            <Button size="sm" className="self-start">
              Add
            </Button>
          </NotAvailable>
        )}
      </div>
    </article>
  );
}
