// The GitLab mark, one monochrome path on a 16 px grid. Like the GitHub mark it takes the text colour
// (currentColor), carries no brand colour and is hidden from screen readers: the text beside it names the host.
import type { SVGProps } from 'react';

export function GitLabMark(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 16 16" width="16" height="16" fill="currentColor" aria-hidden="true" {...props}>
      <path d="M8 15.2.7 9.9 2.3 5 3.9.9a.3.3 0 0 1 .6 0L6.1 5h3.8l1.6-4.1a.3.3 0 0 1 .6 0L13.7 5l1.6 4.9z" />
    </svg>
  );
}
