// A source's documents (spec 4 §10): each with its status and why it failed, a page at a time;
// a document's indexed text (what knowledge__read gives an agent) in a dialog the URL names
// (?document=), with its original: downloaded for an upload, at its source for a synced one.
// Owners and admins delete uploaded documents; a synced one comes back with the next sync.
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { Download, Ellipsis, ExternalLink } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';
import { deleteKnowledgeDocument, documentContentUrl } from '../../api/knowledge';
import { keys, knowledgeDocumentQuery, knowledgeDocumentsQuery } from '../../api/queries';
import type { KnowledgeDocument, KnowledgeSource } from '../../api/types';
import { plural } from '../../shared/format';
import { externalUrl } from '../../shared/knowledge';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { Dialog, DialogContent } from '../../ui/dialog';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../../ui/menu';
import { StatusText } from './parts';
import { documentStatus, formatBytes, refusalText } from './words';

const pageSize = 100;

/** Where a document lives, for the mono line under its title: its path at the source, else its type. */
function documentPath(d: KnowledgeDocument): string {
  const url = externalUrl(d.url);
  if (!url) return d.mime;
  try {
    const path = decodeURIComponent(new URL(url).pathname).replace(/^\/+/, '');
    return path.replace(/^[^/]+\/[^/]+\/(?:blob|tree)\/[^/]+\//, '') || url;
  } catch {
    return url;
  }
}

/** A document's title (opening its text) with its path under it, as the Knowledge source page lists it. */
export function DocumentLine({ source, document: d }: { source: string; document: KnowledgeDocument }) {
  const org = useOrg();
  const path = documentPath(d);
  return (
    <span className="flex min-w-0 flex-col overflow-hidden">
      <Link
        to="/o/$org/knowledge/$source"
        params={{ org: org.slug, source }}
        search={{ document: d.id }}
        title={d.title}
        className="flex min-w-0 max-w-full items-center font-control text-body text-ink hover:text-run-ink max-md:min-h-11"
      >
        <span title={d.title} className="min-w-0 truncate">
          {d.title}
        </span>
      </Link>
      <span translate="no" title={path} className="min-w-0 truncate font-mono text-meta text-muted">
        {path}
      </span>
    </span>
  );
}

/** Every document of the source, a page at a time, with each one's actions. */
export function DocumentList({ source, manage }: { source: KnowledgeSource; manage: boolean }) {
  const org = useOrg();
  const pages = useInfiniteQuery(knowledgeDocumentsQuery(org.id, source.name, pageSize));
  const documents = pages.data?.pages.flatMap((p) => p.documents) ?? [];
  const [deleting, setDeleting] = useState<KnowledgeDocument | null>(null);
  const now = Date.now();

  if (pages.isPending) {
    return (
      <p aria-busy="true" className="m-0 text-body text-muted">
        Loading documents…
      </p>
    );
  }
  if (pages.isError) {
    return (
      <div role="alert" className="flex flex-col items-start gap-2">
        <p className="m-0 text-body text-rework-ink">Couldn’t load the documents.</p>
        <Button variant="white" onClick={() => void pages.refetch()}>
          Try again
        </Button>
      </div>
    );
  }
  if (documents.length === 0) {
    return (
      <p className="m-0 text-body text-muted">
        {source.kind === 'upload'
          ? 'No documents yet. Uploaded files show here, then agents can search them.'
          : 'No documents yet. They show here once its first sync ends.'}
      </p>
    );
  }
  return (
    <>
      <ul aria-label={`Documents of ${source.title}`} className="m-0 flex list-none flex-col p-0">
        {documents.map((d) => (
          <li
            key={d.id}
            className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 border-t border-line py-2 first:border-t-0 first:pt-0 md:grid-cols-[minmax(0,1fr)_auto_4.5rem_auto]"
          >
            <DocumentLine source={source.name} document={d} />
            <div className="order-last col-span-2 flex flex-wrap items-baseline gap-x-3 md:contents">
              <StatusText line={documentStatus(d, now)} className="text-secondary" />
              <span className="text-meta text-muted tabular-nums md:text-right">{formatBytes(d.bytes)}</span>
            </div>
            <DocumentActions
              document={d}
              upload={source.kind === 'upload'}
              manage={manage}
              onDelete={() => setDeleting(d)}
            />
          </li>
        ))}
      </ul>
      {pages.hasNextPage ? (
        <Button
          variant="white"
          className="self-start"
          disabled={pages.isFetchingNextPage}
          onClick={() => void pages.fetchNextPage()}
        >
          {pages.isFetchingNextPage ? 'Loading…' : `Show the next ${pageSize}`}
        </Button>
      ) : null}
      <DeleteDocumentDialog document={deleting} onClose={() => setDeleting(null)} />
    </>
  );
}

function DocumentActions({
  document: d,
  upload,
  manage,
  onDelete,
}: {
  document: KnowledgeDocument;
  upload: boolean;
  manage: boolean;
  onDelete: () => void;
}) {
  const org = useOrg();
  if (!upload) {
    const url = externalUrl(d.url);
    return url ? (
      <Button
        asChild
        variant="ghost"
        size="icon"
        className="size-11 shrink-0 md:size-8"
        aria-label={`Open ${d.title} at its source`}
      >
        <a href={url} target="_blank" rel="noreferrer noopener">
          <ExternalLink size={16} strokeWidth={1.5} aria-hidden="true" />
        </a>
      </Button>
    ) : null;
  }
  return (
    <Menu>
      <MenuTrigger asChild>
        <Button variant="ghost" size="icon" className="size-11 md:size-8" aria-label={`Actions for ${d.title}`}>
          <Ellipsis size={18} strokeWidth={1.5} aria-hidden="true" />
        </Button>
      </MenuTrigger>
      <MenuContent side="bottom" align="end" className="w-56">
        <MenuItem asChild>
          <a href={documentContentUrl(org.id, d.id)} download={d.title}>
            <Download size={16} strokeWidth={1.5} aria-hidden="true" className="text-muted" />
            Download original
          </a>
        </MenuItem>
        {manage ? <MenuItem onSelect={onDelete}>Delete…</MenuItem> : null}
      </MenuContent>
    </Menu>
  );
}

function DeleteDocumentDialog({ document: d, onClose }: { document: KnowledgeDocument | null; onClose: () => void }) {
  const org = useOrg();
  const client = useQueryClient();
  const remove = useMutation({
    mutationFn: (doc: KnowledgeDocument) => deleteKnowledgeDocument(org.id, doc.id),
    onSuccess: async (_, doc) => {
      onClose();
      toast.success(`Deleted ${doc.title}`);
      await client.invalidateQueries({ queryKey: keys.knowledge(org.id) });
    },
  });
  return (
    <ConfirmDialog
      open={d !== null}
      onOpenChange={(open) => {
        if (!open) {
          remove.reset();
          onClose();
        }
      }}
      title={`Delete ${d?.title ?? 'the document'}?`}
      description="Its text leaves the index, so agents can’t find it any more. This can’t be undone."
      confirmLabel="Delete document"
      pendingLabel="Deleting…"
      tone="danger"
      pending={remove.isPending}
      onConfirm={() => {
        if (d) remove.mutate(d);
      }}
    >
      {remove.error ? (
        <p role="alert" className="m-0 text-body text-rework-ink">
          {refusalText(remove.error)}
        </p>
      ) : null}
    </ConfirmDialog>
  );
}

/** The document ?document= names, over the source's page: its indexed text and its original. */
export function DocumentDialog({ source, id }: { source: KnowledgeSource; id: string | undefined }) {
  const org = useOrg();
  const navigate = useNavigate({ from: '/o/$org/knowledge/$source' });
  const read = useQuery({ ...knowledgeDocumentQuery(org.id, id ?? ''), enabled: id !== undefined });
  const close = () => void navigate({ search: (prev) => ({ ...prev, document: undefined }), replace: true });
  const d = read.data?.document;
  const text = read.data?.text ?? '';

  return (
    <Dialog
      open={id !== undefined}
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      {id !== undefined ? (
        <DialogContent
          title={d?.title ?? 'Document'}
          description={d ? `${source.title}, ${formatBytes(d.bytes)}` : undefined}
          className="max-w-[720px]"
        >
          {read.isPending ? (
            <p aria-busy="true" className="m-0 text-body text-muted">
              Loading its text…
            </p>
          ) : read.isError ? (
            <p role="alert" className="m-0 text-body text-rework-ink">
              {refusalText(read.error)}
            </p>
          ) : d ? (
            <>
              <StatusText line={documentStatus(d, Date.now())} className="text-body" />
              {text ? (
                <section
                  // biome-ignore lint/a11y/noNoninteractiveTabindex: a scrolling region must be focusable
                  tabIndex={0}
                  aria-label={`Indexed text of ${d.title}`}
                  className="max-h-[50dvh] overflow-y-auto rounded-control border border-line bg-card p-3 text-body leading-relaxed break-words whitespace-pre-wrap"
                >
                  {text}
                </section>
              ) : (
                <p className="m-0 text-body text-muted">
                  Nothing indexed yet: agents find its text once{' '}
                  {d.status === 'pending' ? 'it is indexed' : 'it has some'}.
                </p>
              )}
              {text ? (
                <p className="m-0 text-secondary text-muted">
                  {plural(text.length, 'character')} indexed.{' '}
                  {source.kind === 'upload'
                    ? 'An agent reads it whole with knowledge__read.'
                    : 'Agents find it a piece at a time with knowledge__search.'}
                </p>
              ) : null}
              <div className="flex flex-wrap justify-end gap-2">
                {source.kind === 'upload' ? (
                  <Button asChild variant="white">
                    <a href={documentContentUrl(org.id, d.id)} download={d.title}>
                      <Download size={16} strokeWidth={1.5} aria-hidden="true" />
                      Download original
                    </a>
                  </Button>
                ) : externalUrl(d.url) ? (
                  <Button asChild variant="white">
                    <a href={externalUrl(d.url)} target="_blank" rel="noreferrer noopener">
                      <ExternalLink size={16} strokeWidth={1.5} aria-hidden="true" />
                      Open at its source
                    </a>
                  </Button>
                ) : null}
              </div>
            </>
          ) : null}
        </DialogContent>
      ) : null}
    </Dialog>
  );
}
