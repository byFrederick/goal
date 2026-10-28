// Create organization (C §10.4): a name and an address that follows the name until it is edited,
// POST /v1/orgs, then the new organization's (empty) Factories home. Opened from the
// organization menu and from the phone's organization sheet.
import { useNavigate } from '@tanstack/react-router';
import { type FormEvent, type RefObject, useRef, useState } from 'react';
import { toast } from 'sonner';
import { isSlugTaken, useCreateOrg } from '../shared/create-org';
import { checkSlug, orgNameMax, orgSlugMax, slugFromName } from '../shared/org-rules';
import { Button } from '../ui/button';
import { DialogClose, DialogContent, Dialog as DialogRoot } from '../ui/dialog';
import { Field, Input } from '../ui/field';

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Where focus returns when the dialog closes without creating (the organization mark). */
  returnFocus: RefObject<HTMLElement | null>;
}

export function CreateOrgDialog({ open, onOpenChange, returnFocus }: Props) {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      {/* Mounted only while open, so every opening starts from an empty form. */}
      {open ? <CreateOrgForm onDone={() => onOpenChange(false)} returnFocus={returnFocus} /> : null}
    </DialogRoot>
  );
}

function CreateOrgForm({ onDone, returnFocus }: { onDone: () => void; returnFocus: RefObject<HTMLElement | null> }) {
  const navigate = useNavigate();
  const nameRef = useRef<HTMLInputElement>(null);
  const slugRef = useRef<HTMLInputElement>(null);
  /** Set once created: the new page's heading takes focus, not the mark. */
  const created = useRef(false);

  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  /** The address follows the name until the person edits it. */
  const [slugEdited, setSlugEdited] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  /** The address goald said is taken, while the field still holds it. */
  const [takenSlug, setTakenSlug] = useState<string | null>(null);

  const nameError = submitted && !name.trim() ? 'Give the organization a name.' : undefined;
  const slugRule = slug === '' ? 'Give the organization an address.' : checkSlug(slug);
  const slugError = submitted
    ? (slugRule ?? (takenSlug === slug ? `${slug} is taken. Choose another address.` : undefined))
    : undefined;

  const create = useCreateOrg((org) => {
    created.current = true;
    onDone();
    toast.success(`Created ${org.name}.`);
    navigate({ to: '/o/$org', params: { org: org.slug } });
  });
  const failed = create.error && !isSlugTaken(create.error);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    setSubmitted(true);
    if (!name.trim()) return nameRef.current?.focus();
    if (slugRule) return slugRef.current?.focus();
    create.mutate(
      { name: name.trim(), slug },
      {
        onError: (err) => {
          if (isSlugTaken(err)) {
            setTakenSlug(slug);
            slugRef.current?.focus();
          }
        },
      },
    );
  };

  return (
    <DialogContent
      title="Create organization"
      description="Factories, connections and settings belong to an organization."
      initialFocus={nameRef}
      onCloseAutoFocus={(e) => {
        e.preventDefault();
        if (!created.current) returnFocus.current?.focus();
      }}
    >
      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        <Field label="Name" error={nameError}>
          <Input
            ref={nameRef}
            value={name}
            maxLength={orgNameMax}
            autoComplete="organization"
            onChange={(e) => {
              setName(e.target.value);
              if (!slugEdited) setSlug(slugFromName(e.target.value));
            }}
          />
        </Field>
        <Field
          label="Address"
          help={
            <>
              Lowercase letters, digits and dashes. Links use it: <span className="text-ink">/o/{slug || '…'}</span>
            </>
          }
          error={slugError}
        >
          <Input
            ref={slugRef}
            value={slug}
            maxLength={orgSlugMax}
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
            onChange={(e) => {
              setSlug(e.target.value.trim());
              setSlugEdited(true);
            }}
          />
        </Field>
        {failed ? (
          <p role="alert" className="m-0 text-sm text-rework-ink">
            Could not create the organization: {create.error.message}
          </p>
        ) : null}
        <div className="flex flex-wrap justify-end gap-2">
          <DialogClose asChild>
            <Button variant="secondary">Cancel</Button>
          </DialogClose>
          <Button type="submit" variant="primary" disabled={create.isPending}>
            {create.isPending ? 'Creating…' : 'Create organization'}
          </Button>
        </div>
      </form>
    </DialogContent>
  );
}
