// "is created" once: a page opened right after a create (?created=true) says so, and the flag
// leaves the URL at once, so a reload or a shared link of the page does not say it again.
import { useNavigate } from '@tanstack/react-router';
import { useEffect, useState } from 'react';

export function useCreatedOnce(created: boolean | undefined): boolean {
  const [shown] = useState(created === true);
  const navigate = useNavigate();
  useEffect(() => {
    if (created) void navigate({ to: '.', search: ({ created: _, ...rest }) => rest, replace: true });
  }, [created, navigate]);
  return shown;
}
