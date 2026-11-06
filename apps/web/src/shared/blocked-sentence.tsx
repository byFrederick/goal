// A block's plain sentence with its fix as a link, for a factory the caller names.
import { Link } from '@tanstack/react-router';
import type { BlockedWords } from './blocked-words';

export function BlockedSentence({ words, org, factory }: { words: BlockedWords; org: string; factory: string }) {
  const { sentence, fix } = words;
  const lead = sentence.endsWith(fix.label) ? sentence.slice(0, -fix.label.length) : `${sentence} `;
  return (
    <>
      {lead}
      <Link to={fix.to} params={{ org, factory }} hash={fix.hash}>
        {fix.label}
      </Link>
    </>
  );
}
