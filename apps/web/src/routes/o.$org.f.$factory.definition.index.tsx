import { createFileRoute, redirect } from '@tanstack/react-router';

// /definition opens the Line.
export const Route = createFileRoute('/o/$org/f/$factory/definition/')({
  beforeLoad: ({ params }) => {
    throw redirect({
      to: '/o/$org/f/$factory/definition/$section',
      params: { ...params, section: 'line' },
      replace: true,
    });
  },
});
