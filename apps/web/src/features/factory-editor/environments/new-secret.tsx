// The New secret sheet opened from a variable, a file or Add a tool: the sheet of the Secrets page, with
// the name of the secret that is created handed to whoever opened it.
import { useQuery } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import { factoriesQuery } from '../../../api/queries';
import { useOrg } from '../../../shared/org';
import { NewSecretSheet } from '../../secrets/new-secret-sheet';

export function NewSecretSheetFor(initial?: { name?: string; value?: string; allFactories?: boolean }) {
  const org = useOrg();
  const factories = useQuery(factoriesQuery(org.id)).data ?? [];
  const [open, setOpen] = useState(false);
  const done = useRef<(name: string) => void>(() => undefined);
  return {
    open: (onCreated: (name: string) => void) => {
      done.current = onCreated;
      setOpen(true);
    },
    sheet: (
      <NewSecretSheet
        org={org.id}
        open={open}
        onOpenChange={setOpen}
        factories={factories}
        initial={initial}
        onCreated={(name) => done.current(name)}
      />
    ),
  };
}
