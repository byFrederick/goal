// Add a tool (ADR 0037 decision 14, canvas build/add-tool, add-tool-768, add-tool-custom, mobile/add-tool):
// one page with the catalog beside the tool picked. On a phone the catalog is the page and a picked tool
// replaces it, with Back to the catalog. The page writes only into the draft's environment file. It
// opens its own draft session: it is not inside the Definition frame.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { ChevronLeft } from 'lucide-react';
import { catalogQuery } from '../../../api/queries';
import { TopBarSlot } from '../../../app/top-bar';
import { useFactory, useFactoryParams } from '../../../shared/factory';
import { useOrg } from '../../../shared/org';
import { holds } from '../../../shared/permissions';
import { Button } from '../../../ui/button';
import { Callout } from '../../../ui/callout';
import { cn } from '../../../ui/cn';
import { descriptionClass, PageHeader } from '../../../ui/page-header';
import { ReadOnly } from '../../../ui/read-only';
import { Skeleton } from '../../../ui/skeleton';
import { DraftSessionProvider } from '../draft/use-draft';
import { defaultEnvironment } from '../environments/env-file';
import { useEnvironment, useEnvironmentNames } from '../environments/use-environments';
import type { ToolKind } from './apply-template';
import { CatalogList } from './catalog-list';
import { CustomPanel } from './custom-panel';
import { TemplatePanel } from './template-panel';

export interface AddToolSearch {
  environment?: string;
  /** The template's name, or custom. */
  template?: string;
  type?: ToolKind;
}

const text = (v: unknown) => (typeof v === 'string' && v !== '' ? v : undefined);

export function validateAddToolSearch(search: Record<string, unknown>): AddToolSearch {
  const out: AddToolSearch = {};
  const environment = text(search.environment);
  const template = text(search.template);
  if (environment) out.environment = environment;
  if (template) out.template = template;
  if (search.type === 'mcp' || search.type === 'cli' || search.type === 'api') out.type = search.type;
  return out;
}

export function AddToolPage() {
  const org = useOrg();
  const factory = useFactory();
  const readOnly = !holds(factory, 'definition:edit');
  return (
    <DraftSessionProvider key={`${factory.id}:${readOnly}`} org={org.id} factory={factory.id} readOnly={readOnly}>
      <AddTool />
    </DraftSessionProvider>
  );
}

function AddTool() {
  const org = useOrg();
  const params = useFactoryParams();
  const search = useSearch({ strict: false }) as AddToolSearch;
  const navigate = useNavigate();
  const { names } = useEnvironmentNames();
  const environment = search.environment ?? names[0] ?? defaultEnvironment;
  const catalog = useQuery(catalogQuery());
  const { spec } = useEnvironment(environment);
  const templates = catalog.data ?? [];
  const selected = search.template ?? templates[0]?.name ?? 'custom';
  const template = selected === 'custom' ? undefined : templates.find((t) => t.name === selected);
  const editable = holds(useFactory(), 'definition:edit');

  const go = (patch: AddToolSearch) =>
    void navigate({
      to: '/o/$org/f/$factory/definition/add-tool',
      params: { org: org.slug, factory: params.factory },
      search: { environment, ...(search.template ? { template: search.template } : {}), ...patch },
      replace: true,
    });

  const cancel = (
    <Button asChild variant="white">
      <Link
        to="/o/$org/f/$factory/definition/$section"
        params={{ org: org.slug, factory: params.factory, section: 'environments' }}
        search={{ environment }}
      >
        Cancel
      </Link>
    </Button>
  );
  const picked = search.template !== undefined;

  return (
    <>
      <TopBarSlot crumb="Add a tool" actions={cancel} />
      <div className="flex min-w-0 flex-col gap-5 px-4 py-5 md:px-8 md:py-6">
        <PageHeader
          title="Add a tool"
          description={
            <p className={cn(descriptionClass, picked && 'max-lg:hidden')}>
              Pick one and Goal writes it into an environment file in your draft. Nothing changes until you publish.
            </p>
          }
        />
        {catalog.error ? (
          <div role="alert" className="flex flex-col items-start gap-3">
            <Callout tone="rework">The catalog did not load.</Callout>
            <Button variant="white" onClick={() => void catalog.refetch()}>
              Try again
            </Button>
          </div>
        ) : (
          <Skeleton ready={catalog.data !== undefined} placeholder={<CatalogPlaceholder />}>
            <ReadOnly when={!editable}>
              <div className="grid min-w-0 overflow-hidden rounded-card border border-line bg-card lg:grid-cols-[minmax(0,320px)_minmax(0,1fr)]">
                <CatalogList
                  templates={templates}
                  selected={selected}
                  environment={environment}
                  spec={spec}
                  className={cn('border-line bg-shell lg:border-r', picked && 'max-lg:hidden')}
                />
                <div className={cn('min-w-0 p-4 md:p-7', !picked && 'max-lg:hidden')}>
                  <Link
                    to="/o/$org/f/$factory/definition/add-tool"
                    params={{ org: org.slug, factory: params.factory }}
                    search={{ environment }}
                    className="press mb-3 inline-flex min-h-11 items-center gap-1 text-body text-ink no-underline hover:text-link lg:hidden"
                  >
                    <ChevronLeft size={16} strokeWidth={1.75} aria-hidden="true" />
                    Tools
                  </Link>
                  {selected === 'custom' ? (
                    <CustomPanel
                      key={search.type ?? 'mcp'}
                      environment={environment}
                      onEnvironment={(e) => go({ environment: e })}
                      type={search.type ?? 'mcp'}
                      onType={(type) => go({ type })}
                    />
                  ) : template ? (
                    <TemplatePanel
                      key={`${template.name}:${environment}`}
                      template={template}
                      environment={environment}
                      onEnvironment={(e) => go({ environment: e })}
                    />
                  ) : (
                    <p className="m-0 text-body text-muted">There is no tool called {selected} in the catalog.</p>
                  )}
                </div>
              </div>
            </ReadOnly>
          </Skeleton>
        )}
      </div>
    </>
  );
}

function CatalogPlaceholder() {
  const block = 'rounded-control bg-hover';
  return (
    <div className="grid min-w-0 gap-4 lg:grid-cols-[320px_minmax(0,1fr)]">
      <div className={cn(block, 'h-96')} />
      <div className={cn(block, 'h-96')} />
    </div>
  );
}
